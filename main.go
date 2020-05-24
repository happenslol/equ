package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

type itemType int
type lexerStateFn func(l *lexer) lexerStateFn
type parserStateFn func(l *parser) parserStateFn

const (
	itemEOF itemType = iota
	itemError
	itemPath
	itemOr
	itemAnd
	itemFiltersStart
	itemFiltersEnd
	itemBundleStart
	itemBundleEnd
	itemString
	itemNumber

	itemFilterGt
	itemFilterGte
	itemFilterLt
	itemFilterLte
	itemFilterEq
	itemFilterCt
)

func (i itemType) String() string {
	switch i {
	case itemEOF:
		return "eof"
	case itemError:
		return "error"
	case itemPath:
		return "path"
	case itemOr:
		return "or"
	case itemAnd:
		return "and"
	case itemFiltersStart:
		return "filtersStart"
	case itemFiltersEnd:
		return "filtersEnd"
	case itemBundleStart:
		return "bundleStart"
	case itemBundleEnd:
		return "bundleEnd"
	case itemString:
		return "string"
	case itemNumber:
		return "number"
	case itemFilterGt:
		return "filterGt"
	case itemFilterGte:
		return "filterGte"
	case itemFilterLt:
		return "filterLt"
	case itemFilterLte:
		return "filterLte"
	case itemFilterEq:
		return "filterEq"
	case itemFilterCt:
		return "filterCt"
	}

	return ""
}

const (
	tokenFiltersStart = "["
	tokenFiltersEnd   = "]"
	tokenOr           = "|"
	tokenAnd          = ","
	tokenBundleStart  = "("
	tokenBundleEnd    = ")"
	tokenFilterIs     = ":"

	tokenGt  = "gt"
	tokenGte = "gte"
	tokenLt  = "lt"
	tokenLte = "lte"
	tokenEq  = "eq"
	tokenCt  = "ct"

	tokenValidPath        = `abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._`
	tokenValidFilterType  = `abcdefghijklmnopqrstuvwxyz`
	tokenValidNumberStart = `.+-0123456789`
	tokenValidNumberDigit = `0123456789`

	eof = rune(0)
)

var filterTypes = map[string]itemType{
	tokenGt:  itemFilterGt,
	tokenGte: itemFilterGte,
	tokenLt:  itemFilterLt,
	tokenLte: itemFilterLte,
	tokenEq:  itemFilterEq,
	tokenCt:  itemFilterCt,
}

type item struct {
	ItemType itemType
	Value    string
}

type lexer struct {
	input  string
	items  chan item
	done   chan bool
	width  int
	start  int
	pos    int
	cancel chan bool
}

func (l *lexer) run() {
	state := lexFilters
	for state != nil {
		state = state(l)
	}

	l.done <- true
}

func (l *lexer) emit(t itemType) bool {
	value := l.input[l.start:l.pos]

	// If we got a string, we need to clear escaped characters
	if t == itemString {
		value = strings.ReplaceAll(value, `\\`, `\`)
		value = strings.ReplaceAll(value, `\"`, `"`)
	}

	select {
	case l.items <- item{t, value}:
		l.start = l.pos
		return true
	case <-l.cancel:
		return false
	}
}

func (l *lexer) next() (r rune) {
	if l.pos >= len(l.input) {
		l.width = 0
		return eof
	}

	r, l.width = utf8.DecodeRuneInString(l.input[l.pos:])
	l.pos += l.width
	return r
}

func (l *lexer) accept(valid string) bool {
	if strings.IndexRune(valid, l.next()) >= 0 {
		return true
	}

	l.backup()
	return false
}

func (l *lexer) peek() rune {
	r := l.next()
	l.backup()
	return r
}

func (l *lexer) acceptRun(valid string) {
	for strings.IndexRune(valid, l.next()) >= 0 {
	}

	l.backup()
}

func (l *lexer) acceptRunUntil(rs ...rune) bool {
	for {
		next := l.next()
		if next == eof {
			l.backup()
			return false
		}

		for _, r := range rs {
			if next == r {
				l.backup()
				return true
			}
		}
	}
}

func (l *lexer) ignore() {
	l.start = l.pos
}

func (l *lexer) backup() {
	l.pos -= l.width
}

func lexFilters(l *lexer) lexerStateFn {
	if string(l.peek()) == tokenBundleStart {
		return lexToken(tokenBundleStart, itemBundleStart, lexFilters)
	}

	l.acceptRun(tokenValidPath)
	if l.pos <= l.start {
		return l.errorf("expected path, got: %s", l.input[l.start:])
	}

	if !l.emit(itemPath) {
		return nil
	}

	if string(l.peek()) != tokenFiltersStart {
		return l.errorf("expected filters start, got: %s", l.input[l.start:])
	}

	return lexToken(tokenFiltersStart, itemFiltersStart, lexInsideFilters)
}

func lexInsideFilters(l *lexer) lexerStateFn {
	if string(l.peek()) == tokenBundleStart {
		return lexToken(tokenBundleStart, itemBundleStart, lexInsideFilters)
	}

	l.acceptRun(tokenValidFilterType)
	nextFilter := l.input[l.start:l.pos]
	if filterType, ok := filterTypes[nextFilter]; ok {
		if !l.emit(filterType) {
			return nil
		}
	} else {
		return l.errorf("expected filter type, got %s", nextFilter)
	}

	if string(l.next()) != tokenFilterIs {
		return l.errorf("expected %s, got %s", tokenFilterIs, l.input[l.start:l.start])
	}

	// Skip the :
	l.ignore()
	return lexFilterValue
}

func lexFilterValue(l *lexer) lexerStateFn {
	next := l.peek()

	if strings.IndexRune(tokenValidNumberStart, next) >= 0 {
		return lexNumberValue
	} else if next == '"' {
		// Skip the opening quotes
		l.next()
		l.ignore()
		return lexStringValue
	}

	return l.errorf("expected filter value, got %s", l.input[l.start:])
}

func lexStringValue(l *lexer) lexerStateFn {
	for {
		found := l.acceptRunUntil('\\', '"')
		if !found {
			return l.errorf("expected string to end, got: %s", l.input[l.start:])
		}

		// If the next rune is only ", the string is closed
		next := l.peek()
		if next == '"' {
			if !l.emit(itemString) {
				return nil
			}

			// Skip the closing quotes
			l.next()
			l.ignore()
			return lexAfterFilterValue
		}

		if next == '\\' {
			// Check what we're escaping. First, skip the \
			l.next()
			next = l.peek()

			// If we're escaping a \ or ", just keep going, they're filtered
			// out when we emit
			if next == '\\' || next == '"' {
				l.next()
			} else {
				return l.errorf("invalid escape sequence: %s", `\`+string(next))
			}
		}
	}
}

func lexNumberValue(l *lexer) lexerStateFn {
	l.accept("+-")

	// We can only have one decimal dot
	decimalOccured := false
	if l.accept(".") {
		// Found it!
		decimalOccured = true
	}

	l.acceptRun(tokenValidNumberDigit)
	if l.accept(".") {
		if decimalOccured {
			// Found a second decimal point
			return l.errorf("invalid number: %s", l.input[l.start:l.pos])
		}

		l.acceptRun(tokenValidNumberDigit)
	}

	if !l.emit(itemNumber) {
		return nil
	}

	return lexAfterFilterValue
}

func lexAfterFilterValue(l *lexer) lexerStateFn {
	next := l.next()
	switch string(next) {
	case tokenOr:
		if !l.emit(itemOr) {
			return nil
		}
	case tokenAnd:
		if !l.emit(itemAnd) {
			return nil
		}
	case tokenFiltersEnd:
		l.backup()
		return lexToken(tokenFiltersEnd, itemFiltersEnd, lexAfterFilter)
	case tokenBundleEnd:
		l.backup()
		return lexToken(tokenBundleEnd, itemBundleEnd, lexAfterFilterValue)
	default:
		return l.errorf("invalid combinator: %s", string(next))
	}

	return lexInsideFilters
}

func lexAfterFilter(l *lexer) lexerStateFn {
	next := l.next()

	if next == eof {
		if !l.emit(itemEOF) {
			return nil
		}
		return nil
	}

	switch string(next) {
	case tokenOr:
		if !l.emit(itemOr) {
			return nil
		}
	case tokenAnd:
		if !l.emit(itemAnd) {
			return nil
		}
	case tokenBundleEnd:
		l.backup()
		return lexToken(tokenBundleEnd, itemBundleEnd, lexAfterFilter)
	default:
		return l.errorf("invalid combinator: %s", string(next))
	}

	return lexFilters
}

func lexToken(tv string, it itemType, next lexerStateFn) func(l *lexer) lexerStateFn {
	return func(l *lexer) lexerStateFn {
		l.pos += len(tv)
		if !l.emit(it) {
			return nil
		}
		return next
	}
}

func (l *lexer) errorf(format string, args ...interface{}) lexerStateFn {
	l.items <- item{
		itemError,
		fmt.Sprintf(format, args...),
	}

	return nil
}

func lex(input string) *lexer {
	l := &lexer{
		input:  input,
		items:  make(chan item, 5),
		done:   make(chan bool, 1),
		cancel: make(chan bool, 1),
	}

	go l.run()
	return l
}

type expressionType int

const (
	expressionTypeEq expressionType = iota
	expressionTypeGt
	expressionTypeGte
	expressionTypeLt
	expressionTypeLte
	expressionTypeCt
)

func (e *expressionType) String() string {
	switch *e {
	case expressionTypeEq:
		return "eq"
	case expressionTypeGt:
		return "gt"
	case expressionTypeGte:
		return "gte"
	case expressionTypeLt:
		return "lt"
	case expressionTypeLte:
		return "lte"
	case expressionTypeCt:
		return "ct"
	default:
		return "et"
	}
}

type filterItem interface {
	isFilterItem()
}

type filterOperand struct {
	path            string
	expressionItems []expressionItem
}

func (f *filterOperand) isFilterItem() {}

type filterOperatorType int

const (
	filterOperatorTypeOr filterOperatorType = iota
	filterOperatorTypeAnd
	filterOperatorTypeBundleStart
	filterOperatorTypeBundleEnd
)

func (f *filterOperatorType) String() string {
	switch *f {
	case filterOperatorTypeOr:
		return "or"
	case filterOperatorTypeAnd:
		return "and"
	case filterOperatorTypeBundleStart:
		return "bundleStart"
	case filterOperatorTypeBundleEnd:
		return "bundleEnd"
	default:
		return "fop"
	}
}

type filterOperator struct {
	operatorType filterOperatorType
}

func (f *filterOperator) isFilterItem() {}

type expressionItem interface {
	isExpressionItem()
}

type expressionValueType int
type expressionOperatorType int

const (
	expressionValueTypeString expressionValueType = iota
	expressionValueTypeNumber

	expressionOperatorTypeOr expressionOperatorType = iota
	expressionOperatorTypeAnd
	expressionOperatorTypeBundleStart
	expressionOperatorTypeBundleEnd
)

func (e *expressionOperatorType) String() string {
	switch *e {
	case expressionOperatorTypeOr:
		return "or"
	case expressionOperatorTypeAnd:
		return "and"
	case expressionOperatorTypeBundleStart:
		return "bundleStart"
	case expressionOperatorTypeBundleEnd:
		return "bundleEnd"
	default:
		return "eop"
	}
}

type expressionOperand struct {
	expressionType expressionType
	valueType      expressionValueType
	value          interface{}
}

type expressionOperator struct {
	operatorType expressionOperatorType
}

func (e *expressionOperator) isExpressionItem() {}
func (e *expressionOperand) isExpressionItem()  {}

func (p *parser) popFilterOperator() *filterOperator {
	if len(p.filterOperatorStack) == 0 {
		return nil
	}

	var item *filterOperator
	item, p.filterOperatorStack = p.filterOperatorStack[len(p.filterOperatorStack)-1], p.filterOperatorStack[:len(p.filterOperatorStack)-1]
	return item
}

func (p *parser) pushFilterOperator(f *filterOperator) {
	p.filterOperatorStack = append(p.filterOperatorStack, f)
}

func (p *parser) pushFilterItems(f ...filterItem) {
	p.filterItems = append(p.filterItems, f...)
}

func (p *parser) popExpressionOperator() *expressionOperator {
	if len(p.expressionOperatorStack) == 0 {
		return nil
	}

	var item *expressionOperator
	item, p.expressionOperatorStack = p.expressionOperatorStack[len(p.expressionOperatorStack)-1], p.expressionOperatorStack[:len(p.expressionOperatorStack)-1]
	return item
}

func (p *parser) pushExpressionOperator(f *expressionOperator) {
	p.expressionOperatorStack = append(p.expressionOperatorStack, f)
}

func (p *parser) pushExpressionItem(e expressionItem) {
	top := p.currentFilterOperand()
	if top == nil {
		panic("invalid codepath")
	}

	top.expressionItems = append(top.expressionItems, e)
}

func (p *parser) topFilterOperator() *filterOperator {
	if len(p.filterOperatorStack) == 0 {
		return nil
	}

	return p.filterOperatorStack[len(p.filterOperatorStack)-1]
}

func (p *parser) topExpressionOperator() *expressionOperator {
	if len(p.expressionOperatorStack) == 0 {
		return nil
	}

	return p.expressionOperatorStack[len(p.expressionOperatorStack)-1]
}

func (p *parser) currentFilterOperand() *filterOperand {
	if len(p.filterItems) == 0 {
		return nil
	}

	top := p.filterItems[len(p.filterItems)-1]
	topOperand, ok := top.(*filterOperand)
	if !ok {
		return nil
	}

	return topOperand
}

func (p *parser) currentExpressionOperand() *expressionOperand {
	if len(p.filterItems) == 0 {
		return nil
	}

	topFilter := p.filterItems[len(p.filterItems)-1]
	topFilterOperand, ok := topFilter.(*filterOperand)
	if !ok {
		return nil
	}

	if len(topFilterOperand.expressionItems) == 0 {
		return nil
	}

	top := topFilterOperand.expressionItems[len(topFilterOperand.expressionItems)-1]
	topOperand, ok := top.(*expressionOperand)
	if !ok {
		return nil
	}

	return topOperand
}

func (i item) toExpressionType() expressionType {
	switch i.ItemType {
	case itemFilterEq:
		return expressionTypeEq
	case itemFilterGt:
		return expressionTypeGt
	case itemFilterGte:
		return expressionTypeGte
	case itemFilterLt:
		return expressionTypeLt
	case itemFilterLte:
		return expressionTypeLte
	case itemFilterCt:
		return expressionTypeCt
	}

	return expressionTypeEq
}

func (i item) toFilterOperator() *filterOperator {
	var operatorType filterOperatorType
	switch i.ItemType {
	case itemAnd:
		operatorType = filterOperatorTypeAnd
	case itemOr:
		operatorType = filterOperatorTypeOr
	case itemBundleStart:
		operatorType = filterOperatorTypeBundleStart
	case itemBundleEnd:
		operatorType = filterOperatorTypeBundleEnd
	}

	return &filterOperator{
		operatorType: operatorType,
	}
}

func (i item) toExpressionOperator() *expressionOperator {
	var operatorType expressionOperatorType
	switch i.ItemType {
	case itemAnd:
		operatorType = expressionOperatorTypeAnd
	case itemOr:
		operatorType = expressionOperatorTypeOr
	case itemBundleStart:
		operatorType = expressionOperatorTypeBundleStart
	case itemBundleEnd:
		operatorType = expressionOperatorTypeBundleEnd
	}

	return &expressionOperator{
		operatorType: operatorType,
	}
}

type parser struct {
	filterOperatorStack     []*filterOperator
	expressionOperatorStack []*expressionOperator
	done                    chan bool
	items                   <-chan item
	cancelLexer             chan<- bool

	filterItems []filterItem
	err         error
}

func (p *parser) errorf(format string, args ...interface{}) parserStateFn {
	p.err = fmt.Errorf(format, args...)
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

func parse(items <-chan item, lexerDone <-chan bool, cancelLexer chan<- bool) ([]filterItem, error) {
	p := parser{
		items:                   items,
		filterOperatorStack:     []*filterOperator{},
		expressionOperatorStack: []*expressionOperator{},
		done:                    make(chan bool, 1),
		filterItems:             []filterItem{},
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
		p.pushFilterOperator(&filterOperator{filterOperatorTypeBundleStart})
		return parseFilters
	}

	if next.ItemType == itemPath {
		p.pushFilterItems(&filterOperand{
			path:            next.Value,
			expressionItems: []expressionItem{},
		})

		return parseFilterType
	}

	return p.errorf("expected bundle start or path, got %s", next.ItemType.String())
}

func parseFilterType(p *parser) parserStateFn {
	next := <-p.items
	if next.ItemType == itemFiltersStart {
		return parseExpression
	}

	return p.errorf("expected filters start, got %s", next.ItemType.String())
}

func parseExpression(p *parser) parserStateFn {
	next := <-p.items
	if next.ItemType == itemBundleStart {
		p.pushExpressionOperator(next.toExpressionOperator())
		return parseExpression
	}

	expressionType := next.toExpressionType()

	var value interface{}
	var valueType expressionValueType

	next = <-p.items
	switch next.ItemType {
	case itemString:
		value = next.Value
		valueType = expressionValueTypeString
		break
	case itemNumber:
		num, err := strconv.ParseFloat(next.Value, 64)
		if err != nil {
			return p.errorf("failed to parse number: %s", err.Error())
		}

		value = num
		valueType = expressionValueTypeNumber
		break
	default:
		return p.errorf("expected string or number, got %s", next.ItemType.String())
	}

	p.pushExpressionItem(&expressionOperand{
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
			return p.errorf("found open expression bundle")
		}

		current.expressionItems = append(current.expressionItems, operators...)
		p.expressionOperatorStack = []*expressionOperator{}
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
			if op.operatorType == expressionOperatorTypeBundleStart {
				startFound = true
				break
			}

			p.pushExpressionItem(op)
			op = p.popExpressionOperator()
		}

		if !startFound {
			return p.errorf("no bundle start found for end")
		}

		return parseAfterExpression
	}

	return p.errorf("expected filters end, or/and or bundle end, got %s", next.ItemType.String())
}

func parseAfterFilter(p *parser) parserStateFn {
	next := <-p.items
	if next.ItemType == itemEOF {
		operators := reverseFilterOperators(p.filterOperatorStack)
		if hasOpenFilterBundle(operators) {
			return p.errorf("found open filter bundle")
		}

		p.pushFilterItems(operators...)
		p.filterOperatorStack = []*filterOperator{}
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
			return p.errorf("no bundle start found for end")
		}

		return parseAfterFilter
	}

	return p.errorf("expected eof, or/and or bundle end, got %s", next.ItemType.String())
}

func reverseExpressionOperators(items []*expressionOperator) []expressionItem {
	itemsLen := len(items)
	result := make([]expressionItem, itemsLen)

	for i, item := range items {
		itemCopy := *item
		result[itemsLen-i-1] = &itemCopy
	}

	return result
}

func reverseFilterOperators(items []*filterOperator) []filterItem {
	itemsLen := len(items)
	result := make([]filterItem, itemsLen)

	for i, item := range items {
		itemCopy := *item
		result[itemsLen-i-1] = &itemCopy
	}

	return result
}

func hasOpenExpressionBundle(items []expressionItem) bool {
	for _, item := range items {
		op, ok := item.(*expressionOperator)
		if !ok {
			continue
		}

		if op.operatorType == expressionOperatorTypeBundleStart {
			return true
		}
	}

	return false
}

func hasOpenFilterBundle(items []filterItem) bool {
	for _, item := range items {
		op, ok := item.(*filterOperator)
		if !ok {
			continue
		}

		if op.operatorType == filterOperatorTypeBundleStart {
			return true
		}
	}

	return false
}

func printResult(items []filterItem) {
	for _, f := range items {
		switch f.(type) {
		case *filterOperand:
			fmt.Printf("filter{ ")
			op := f.(*filterOperand)
			for _, e := range op.expressionItems {
				switch e.(type) {
				case *expressionOperand:
					eop := e.(*expressionOperand)
					fmt.Printf("e(%s %v) ", eop.expressionType.String(), eop.value)
				case *expressionOperator:
					eop := e.(*expressionOperator)
					fmt.Printf("op(%s) ", eop.operatorType.String())
				}
			}

			fmt.Printf("} ")

		case *filterOperator:
			op := f.(*filterOperator)
			fmt.Printf("fop(%s) ", op.operatorType.String())
		}
	}
}

func main() {
	testString := `(name.first[eq:"foo"|eq:"bar"]|email[ct:"foo",ct:"bar"]),age[(gt:4.5,lt:-10)|eq:15]`

	l := lex(testString)
	result, err := parse(l.items, l.done, l.cancel)
	if err != nil {
		fmt.Printf("error parsing: %s", err.Error())
	}

	printResult(result)
}
