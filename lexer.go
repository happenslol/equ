package equ

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

type lexerStateFn func(l *lexer) lexerStateFn

const errorContextLength = 5

const (
	tokenFiltersStart = "["
	tokenFiltersEnd   = "]"
	tokenOr           = "|"
	tokenAnd          = ","
	tokenBundleStart  = "("
	tokenBundleEnd    = ")"
	tokenFilterIs     = ":"

	tokenValidPath        = `abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789._`
	tokenValidFilterType  = `abcdefghijklmnopqrstuvwxyz`
	tokenValidNumberStart = `.+-0123456789`
	tokenValidNumberDigit = `0123456789`

	eof = rune(0)
)

type lexer struct {
	input  string
	items  chan Item
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

func (l *lexer) emit(t ItemType) bool {
	value := l.input[l.start:l.pos]

	// If we got a string, we need to clear escaped characters
	if t == itemString {
		value = strings.ReplaceAll(value, `\\`, `\`)
		value = strings.ReplaceAll(value, `\"`, `"`)
	}

	select {
	case l.items <- Item{t, value, l.start}:
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
		return l.errorf(l.currentContext(), itemPath.String())
	}

	if !l.emit(itemPath) {
		return nil
	}

	if string(l.peek()) != tokenFiltersStart {
		return l.errorf(l.currentContext(), tokenFiltersStart)
	}

	return lexToken(tokenFiltersStart, itemFiltersStart, lexInsideFilters)
}

func lexInsideFilters(l *lexer) lexerStateFn {
	if string(l.peek()) == tokenBundleStart {
		return lexToken(tokenBundleStart, itemBundleStart, lexInsideFilters)
	}

	l.acceptRun(tokenValidFilterType)
	if !l.emit(itemFilter) {
		return nil
	}

	if string(l.next()) != tokenFilterIs {
		return l.errorf(l.currentContext(), tokenFilterIs)
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

	return l.errorf(l.currentContext(), itemFilter.String())
}

func lexStringValue(l *lexer) lexerStateFn {
	for {
		found := l.acceptRunUntil('\\', '"')
		if !found {
			return l.errorf(l.currentContext(), itemString.String())
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
				return l.errorf(`\`+string(next), "escapeSequence")
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
			return l.errorf(l.input[l.start:l.pos], itemNumber.String())
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
		return l.errorf(string(next), "combinator")
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
		return l.errorf(string(next), "combinator")
	}

	return lexFilters
}

func lexToken(tv string, it ItemType, next lexerStateFn) func(l *lexer) lexerStateFn {
	return func(l *lexer) lexerStateFn {
		l.pos += len(tv)
		if !l.emit(it) {
			return nil
		}
		return next
	}
}

func (l *lexer) errorf(context string, expected ...string) lexerStateFn {
	var msg string
	if len(expected) == 1 {
		msg = fmt.Sprintf("expected: `%s`", expected[0])
	} else {
		msg = fmt.Sprintf("expected one of: `%s`", strings.Join(expected, ","))
	}

	msg = fmt.Sprintf("%s, got `%s`", msg, context)

	l.items <- Item{itemError, msg, l.pos}
	return nil
}

func (l *lexer) currentContext() string {
	context := l.input[l.start:]

	if len(context) > errorContextLength {
		return context[:errorContextLength]
	}

	return context
}

func lex(input string) *lexer {
	l := &lexer{
		input:  input,
		items:  make(chan Item, 5),
		done:   make(chan bool, 1),
		cancel: make(chan bool, 1),
	}

	go l.run()
	return l
}
