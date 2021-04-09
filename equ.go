package equ

import (
	"fmt"
	"strings"
)

func Parse(input string) ([]FilterItem, error) {
	l := lex(input)
	return parse(l.items, l.done, l.cancel)
}

func Format(items []FilterItem) string {
	var b strings.Builder

	for _, f := range items {
		switch f.(type) {
		case *FilterOperand:
			b.WriteString("filter{ ")
			op := f.(*FilterOperand)
			for _, e := range op.expressionItems {
				switch e.(type) {
				case *ExpressionOperand:
					eop := e.(*ExpressionOperand)
					b.WriteString(fmt.Sprintf("e(%s %v) ", eop.expressionType, eop.value))
				case *ExpressionOperator:
					eop := e.(*ExpressionOperator)
					b.WriteString(fmt.Sprintf("op(%s) ", eop.operatorType.String()))
				}
			}

			b.WriteString("} ")

		case *FilterOperator:
			op := f.(*FilterOperator)
			b.WriteString(fmt.Sprintf("fop(%s) ", op.operatorType.String()))
		}
	}

	return b.String()
}

func ToMap(items []FilterItem) map[string]interface{} {
	rest, start := popFilter(items)
	_, m := convertOperand(start, rest)
	return m
}

func popFilter(items []FilterItem) ([]FilterItem, FilterItem) {
	if len(items) == 0 {
		return nil, nil
	}

	var item FilterItem
	item, items = items[len(items)-1], items[:len(items)-1]
	return items, item
}

func convertOperand(item FilterItem, input []FilterItem) ([]FilterItem, map[string]interface{}) {
	switch item.(type) {
	case *FilterOperator:
		return convertOperator(item.(*FilterOperator), input)
	case *FilterOperand:
		return input, convertExpressions(item.(*FilterOperand))
	}

	return input, nil
}

func convertOperator(item *FilterOperator, input []FilterItem) ([]FilterItem, map[string]interface{}) {
	var op1, op2 map[string]interface{}

	rest, start := popFilter(input)
	rest, op1 = convertOperand(start, rest)

	rest, start = popFilter(rest)
	rest, op2 = convertOperand(start, rest)

	if item.operatorType == FilterOperatorTypeAnd {
		return rest, map[string]interface{}{"$and": []map[string]interface{}{op1, op2}}
	}

	if item.operatorType == FilterOperatorTypeOr {
		return rest, map[string]interface{}{"$or": []map[string]interface{}{op1, op2}}
	}

	return rest, nil
}

func popExpression(items []ExpressionItem) ([]ExpressionItem, ExpressionItem) {
	if len(items) == 0 {
		return nil, nil
	}

	var item ExpressionItem
	item, items = items[len(items)-1], items[:len(items)-1]
	return items, item
}

func convertExpressions(item *FilterOperand) map[string]interface{} {
	rest, start := popExpression(item.expressionItems)
	_, result := convertExpressionOperand(start, item.path, rest)
	return result
}

func convertExpressionOperator(operator *ExpressionOperator, path string, expressions []ExpressionItem) ([]ExpressionItem, map[string]interface{}) {
	var op1, op2 map[string]interface{}

	rest, start := popExpression(expressions)
	rest, op1 = convertExpressionOperand(start, path, rest)

	rest, start = popExpression(rest)
	rest, op2 = convertExpressionOperand(start, path, rest)

	if operator.operatorType == ExpressionOperatorTypeAnd {
		return rest, map[string]interface{}{"$and": []map[string]interface{}{op1, op2}}
	}

	if operator.operatorType == ExpressionOperatorTypeOr {
		return rest, map[string]interface{}{"$or": []map[string]interface{}{op1, op2}}
	}

	return rest, nil
}

func convertExpressionOperand(item ExpressionItem, path string, expressions []ExpressionItem) ([]ExpressionItem, map[string]interface{}) {
	switch item.(type) {
	case *ExpressionOperator:
		return convertExpressionOperator(item.(*ExpressionOperator), path, expressions)
	case *ExpressionOperand:
		eop := item.(*ExpressionOperand)
		switch eop.expressionType {
		case "eq":
			return expressions, map[string]interface{}{"$eq": eop.value}
		case "gt":
			return expressions, map[string]interface{}{"$gt": eop.value}
		case "gte":
			return expressions, map[string]interface{}{"$lt": eop.value}
		case "lt":
			return expressions, map[string]interface{}{"$lte": eop.value}
		case "lte":
			return expressions, map[string]interface{}{"$ct": eop.value}
		case "ct":
			return expressions, map[string]interface{}{"$ct": eop.value}
		case "neq":
			return expressions, map[string]interface{}{"$neq": eop.value}
		case "ex":
			return expressions, map[string]interface{}{"$ex": eop.value}
		}
	}

	return expressions, nil
}
