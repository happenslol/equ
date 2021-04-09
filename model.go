package equ

type ItemType int

const (
	itemEOF ItemType = iota
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
	itemFilter
)

func (i ItemType) String() string {
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
  case itemFilter:
    return "filterExpression"
	}

	return ""
}

type Item struct {
	ItemType ItemType
	Value    string
	Pos      int
}

type FilterItem interface {
	isFilterItem()
}

type FilterOperand struct {
	path            string
	expressionItems []ExpressionItem
}

func (f *FilterOperand) isFilterItem() {}

type filterOperatorType int

const (
	FilterOperatorTypeOr filterOperatorType = iota
	FilterOperatorTypeAnd
	filterOperatorTypeBundleStart
	filterOperatorTypeBundleEnd
)

func (f *filterOperatorType) String() string {
	switch *f {
	case FilterOperatorTypeOr:
		return "or"
	case FilterOperatorTypeAnd:
		return "and"
	case filterOperatorTypeBundleStart:
		return "bundleStart"
	case filterOperatorTypeBundleEnd:
		return "bundleEnd"
	default:
		return "fop"
	}
}

type FilterOperator struct {
	operatorType filterOperatorType
}

func (f *FilterOperator) isFilterItem() {}

type ExpressionItem interface {
	isExpressionItem()
}

type ExpressionValueType int
type ExpressionOperatorType int

const (
	ExpressionValueTypeString ExpressionValueType = iota
	ExpressionValueTypeNumber

	ExpressionOperatorTypeOr ExpressionOperatorType = iota
	ExpressionOperatorTypeAnd
	ExpressionOperatorTypeBundleStart
	ExpressionOperatorTypeBundleEnd
)

func (e *ExpressionOperatorType) String() string {
	switch *e {
	case ExpressionOperatorTypeOr:
		return "or"
	case ExpressionOperatorTypeAnd:
		return "and"
	case ExpressionOperatorTypeBundleStart:
		return "bundleStart"
	case ExpressionOperatorTypeBundleEnd:
		return "bundleEnd"
	default:
		return "eop"
	}
}

type ExpressionOperand struct {
	expressionType string
	valueType      ExpressionValueType
	value          interface{}
}

type ExpressionOperator struct {
	operatorType ExpressionOperatorType
}

func (e *ExpressionOperator) isExpressionItem() {}
func (e *ExpressionOperand) isExpressionItem()  {}
