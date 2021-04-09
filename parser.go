package equ

import (
	"fmt"
	"strconv"
	"strings"
)

var _ error = &ParserError{}

type ParserError struct {
	Message string
	Pos     int
}

func (err *ParserError) Error() string {
	return fmt.Sprintf("%s (pos %d)", err.Message, err.Pos)
}

type parserStateFn func(l *parser) parserStateFn

type parser struct {
	filterOperatorStack     []*FilterOperator
	expressionOperatorStack []*ExpressionOperator
	done                    chan bool
	items                   <-chan Item
	cancelLexer             chan<- bool

	filterItems []FilterItem
	err         error
}

func (p *parser) popFilterOperator() *FilterOperator {
	if len(p.filterOperatorStack) == 0 {
		return nil
	}

	var item *FilterOperator
	item, p.filterOperatorStack = p.filterOperatorStack[len(p.filterOperatorStack)-1], p.filterOperatorStack[:len(p.filterOperatorStack)-1]
	return item
}

func (p *parser) pushFilterOperator(f *FilterOperator) {
	p.filterOperatorStack = append(p.filterOperatorStack, f)
}

func (p *parser) pushFilterItems(f ...FilterItem) {
	p.filterItems = append(p.filterItems, f...)
}

func (p *parser) popExpressionOperator() *ExpressionOperator {
	if len(p.expressionOperatorStack) == 0 {
		return nil
	}

	var item *ExpressionOperator
	item, p.expressionOperatorStack = p.expressionOperatorStack[len(p.expressionOperatorStack)-1], p.expressionOperatorStack[:len(p.expressionOperatorStack)-1]
	return item
}

func (p *parser) pushExpressionOperator(f *ExpressionOperator) {
	p.expressionOperatorStack = append(p.expressionOperatorStack, f)
}

func (p *parser) pushExpressionItem(e ExpressionItem) {
	top := p.currentFilterOperand()
	if top == nil {
		panic("invalid codepath")
	}

	top.expressionItems = append(top.expressionItems, e)
}

func (p *parser) topFilterOperator() *FilterOperator {
	if len(p.filterOperatorStack) == 0 {
		return nil
	}

	return p.filterOperatorStack[len(p.filterOperatorStack)-1]
}

func (p *parser) topExpressionOperator() *ExpressionOperator {
	if len(p.expressionOperatorStack) == 0 {
		return nil
	}

	return p.expressionOperatorStack[len(p.expressionOperatorStack)-1]
}

func (p *parser) currentFilterOperand() *FilterOperand {
	if len(p.filterItems) == 0 {
		return nil
	}

	top := p.filterItems[len(p.filterItems)-1]
	topOperand, ok := top.(*FilterOperand)
	if !ok {
		return nil
	}

	return topOperand
}

func (p *parser) currentExpressionOperand() *ExpressionOperand {
	if len(p.filterItems) == 0 {
		return nil
	}

	topFilter := p.filterItems[len(p.filterItems)-1]
	topFilterOperand, ok := topFilter.(*FilterOperand)
	if !ok {
		return nil
	}

	if len(topFilterOperand.expressionItems) == 0 {
		return nil
	}

	top := topFilterOperand.expressionItems[len(topFilterOperand.expressionItems)-1]
	topOperand, ok := top.(*ExpressionOperand)
	if !ok {
		return nil
	}

	return topOperand
}

func (i Item) toFilterOperator() *FilterOperator {
	var operatorType filterOperatorType
	switch i.ItemType {
	case itemAnd:
		operatorType = FilterOperatorTypeAnd
	case itemOr:
		operatorType = FilterOperatorTypeOr
	case itemBundleStart:
		operatorType = filterOperatorTypeBundleStart
	case itemBundleEnd:
		operatorType = filterOperatorTypeBundleEnd
	}

	return &FilterOperator{
		operatorType: operatorType,
	}
}

func (i Item) toExpressionOperator() *ExpressionOperator {
	var operatorType ExpressionOperatorType
	switch i.ItemType {
	case itemAnd:
		operatorType = ExpressionOperatorTypeAnd
	case itemOr:
		operatorType = ExpressionOperatorTypeOr
	case itemBundleStart:
		operatorType = ExpressionOperatorTypeBundleStart
	case itemBundleEnd:
		operatorType = ExpressionOperatorTypeBundleEnd
	}

	return &ExpressionOperator{
		operatorType: operatorType,
	}
}

func (p *parser) errorf(got Item, expected ...string) parserStateFn {
	var msg string
	if got.ItemType == itemError {
		msg = got.Value
	} else {
		if len(expected) == 1 {
			msg = fmt.Sprintf("expected: `%s`", expected[0])
		} else {
			msg = fmt.Sprintf("expected one of: `%s`", strings.Join(expected, ","))
		}
	}

	p.err = &ParserError{
		Message: msg,
		Pos:     got.Pos,
	}

	p.cancelLexer <- true
	return nil
}

func (p *parser) run() {
	state := parseFilters
	for state != nil {
		state = state(p)
	}

	p.done <- true
}

func parse(items <-chan Item, lexerDone <-chan bool, cancelLexer chan<- bool) ([]FilterItem, error) {
	p := parser{
		items:                   items,
		filterOperatorStack:     []*FilterOperator{},
		expressionOperatorStack: []*ExpressionOperator{},
		done:                    make(chan bool, 1),
		filterItems:             []FilterItem{},
		cancelLexer:             cancelLexer,
	}

	go p.run()
	<-lexerDone
	<-p.done

	return p.filterItems, p.err
}

func parseFilters(p *parser) parserStateFn {
	next := <-p.items
	if next.ItemType == itemBundleStart {
		p.pushFilterOperator(&FilterOperator{filterOperatorTypeBundleStart})
		return parseFilters
	}

	if next.ItemType == itemPath {
		p.pushFilterItems(&FilterOperand{
			path:            next.Value,
			expressionItems: []ExpressionItem{},
		})

		return parseFilterType
	}

  return p.errorf(next, itemBundleStart.String(), itemPath.String())
}

func parseFilterType(p *parser) parserStateFn {
	next := <-p.items
	if next.ItemType == itemFiltersStart {
		return parseExpression
	}

  return p.errorf(next, itemFiltersStart.String())
}

func parseExpression(p *parser) parserStateFn {
	next := <-p.items
	if next.ItemType == itemBundleStart {
		p.pushExpressionOperator(next.toExpressionOperator())
		return parseExpression
	}

	expressionType := next.Value

	var value interface{}
	var valueType ExpressionValueType

	next = <-p.items
	switch next.ItemType {
	case itemString:
		value = next.Value
		valueType = ExpressionValueTypeString
		break
	case itemNumber:
		num, err := strconv.ParseFloat(next.Value, 64)
		if err != nil {
      return p.errorf(next, itemNumber.String())
		}

		value = num
		valueType = ExpressionValueTypeNumber
		break
	default:
    return p.errorf(next, itemString.String(), itemNumber.String())
	}

	p.pushExpressionItem(&ExpressionOperand{
		expressionType: expressionType,
		valueType:      valueType,
		value:          value,
	})

	return parseAfterExpression
}

func parseAfterExpression(p *parser) parserStateFn {
	next := <-p.items
	if next.ItemType == itemFiltersEnd {
		current := p.currentFilterOperand()
		operators := reverseExpressionOperators(p.expressionOperatorStack)
		if hasOpenExpressionBundle(operators) {
      return p.errorf(next, itemBundleEnd.String())
		}

		current.expressionItems = append(current.expressionItems, operators...)
		p.expressionOperatorStack = []*ExpressionOperator{}
		return parseAfterFilter
	}

	if next.ItemType == itemOr || next.ItemType == itemAnd {
		op := next.toExpressionOperator()
		topExpression := p.topExpressionOperator()
		for topExpression != nil && op.operatorType > topExpression.operatorType {
			top := p.popExpressionOperator()
			p.pushExpressionItem(top)
			topExpression = p.topExpressionOperator()
		}

		p.pushExpressionOperator(op)
		return parseExpression
	}

	if next.ItemType == itemBundleEnd {
		startFound := false
		op := p.popExpressionOperator()
		for op != nil {
			if op.operatorType == ExpressionOperatorTypeBundleStart {
				startFound = true
				break
			}

			p.pushExpressionItem(op)
			op = p.popExpressionOperator()
		}

		if !startFound {
      return p.errorf(next, itemBundleStart.String())
		}

		return parseAfterExpression
	}

  return p.errorf(
    next,
    itemFiltersEnd.String(),
    itemOr.String(),
    itemAnd.String(),
    itemBundleEnd.String(),
  )
}

func parseAfterFilter(p *parser) parserStateFn {
	next := <-p.items
	if next.ItemType == itemEOF {
		operators := reverseFilterOperators(p.filterOperatorStack)
		if hasOpenFilterBundle(operators) {
      return p.errorf(next, itemFiltersEnd.String())
		}

		p.pushFilterItems(operators...)
		p.filterOperatorStack = []*FilterOperator{}
		return nil
	}

	if next.ItemType == itemOr || next.ItemType == itemAnd {
		op := next.toFilterOperator()
		topFilter := p.topFilterOperator()
		for topFilter != nil && op.operatorType > topFilter.operatorType {
			top := p.popFilterOperator()
			p.pushFilterItems(top)
			topFilter = p.topFilterOperator()
		}

		p.pushFilterOperator(op)
		return parseFilters
	}

	if next.ItemType == itemBundleEnd {
		startFound := false
		op := p.popFilterOperator()
		for op != nil {
			if op.operatorType == filterOperatorTypeBundleStart {
				startFound = true
				break
			}

			p.pushFilterItems(op)
			op = p.popFilterOperator()
		}

		if !startFound {
      return p.errorf(next, itemBundleStart.String())
		}

		return parseAfterFilter
	}

  return p.errorf(
    next,
    itemEOF.String(),
    itemOr.String(),
    itemAnd.String(),
    itemBundleEnd.String(),
  )
}

func reverseExpressionOperators(items []*ExpressionOperator) []ExpressionItem {
	itemsLen := len(items)
	result := make([]ExpressionItem, itemsLen)

	for i, item := range items {
		itemCopy := *item
		result[itemsLen-i-1] = &itemCopy
	}

	return result
}

func reverseFilterOperators(items []*FilterOperator) []FilterItem {
	itemsLen := len(items)
	result := make([]FilterItem, itemsLen)

	for i, item := range items {
		itemCopy := *item
		result[itemsLen-i-1] = &itemCopy
	}

	return result
}

func hasOpenExpressionBundle(items []ExpressionItem) bool {
	for _, item := range items {
		op, ok := item.(*ExpressionOperator)
		if !ok {
			continue
		}

		if op.operatorType == ExpressionOperatorTypeBundleStart {
			return true
		}
	}

	return false
}

func hasOpenFilterBundle(items []FilterItem) bool {
	for _, item := range items {
		op, ok := item.(*FilterOperator)
		if !ok {
			continue
		}

		if op.operatorType == filterOperatorTypeBundleStart {
			return true
		}
	}

	return false
}
