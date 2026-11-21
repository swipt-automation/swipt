package selector

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"

	"github.com/sagernet/sing/common"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/swipt-automation/swipt/internal/fault"
)

var relations = []string{"within", "parent", "has", "not"}

var ErrScripted = E.New("the selector holds a function or a regular expression only JavaScript can run")

type Callbacks[T any] interface {
	Predicate(id int, element T) (bool, error)
	Match(id int, text string) (bool, error)
}

type Vocabulary[T comparable] struct {
	Fields  []Field[T]
	Ref     func(T) string
	Parent  func(T) T
	Repeats func(element, ancestor T) bool
	Plain   func(string) string
}

type Field[T any] struct {
	name  string
	text  func(T) (string, bool)
	flag  func(T) bool
	state func(T) *bool
	set   func(T) []string
}

func Text[T any](name string, value func(T) (string, bool)) Field[T] {
	return Field[T]{name: name, text: value}
}

func Flag[T any](name string, value func(T) bool) Field[T] {
	return Field[T]{name: name, flag: value}
}

func State[T any](name string, value func(T) *bool) Field[T] {
	return Field[T]{name: name, state: value}
}

func Set[T any](name string, value func(T) []string) Field[T] {
	return Field[T]{name: name, set: value}
}

type Selector[T comparable] struct {
	vocabulary *Vocabulary[T]
	predicate  int
	clauses    []clause[T]
	index      *int
	scripted   bool
}

type clause[T comparable] struct {
	name     string
	field    Field[T]
	text     *textMatch
	set      []string
	flag     bool
	relation *Selector[T]
	element  string
}

type textMatch struct {
	text    string
	source  string
	flags   string
	script  int
	regular bool
}

func Parse[T comparable](vocabulary *Vocabulary[T], data []byte) (*Selector[T], error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	selector, err := parseSelector(vocabulary, decoder)
	if err != nil {
		return nil, err
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, E.Extend(fault.ErrInvalid, "the selector is not one JSON value")
	}
	return selector, nil
}

func parseSelector[T comparable](vocabulary *Vocabulary[T], decoder *json.Decoder) (*Selector[T], error) {
	fields, err := readObject(decoder, "expected a selector: an object of element fields, or a function of an element")
	if err != nil {
		return nil, err
	}
	if id, isPredicate := fields.number("$predicate"); isPredicate {
		return &Selector[T]{vocabulary: vocabulary, predicate: id, scripted: true}, nil
	}
	selector := &Selector[T]{vocabulary: vocabulary, predicate: -1}
	for _, entry := range fields {
		if entry.key == "index" {
			var at json.Number
			if json.Unmarshal(entry.value, &at) != nil {
				return nil, E.Extend(fault.ErrInvalid, "index must be a non-negative integer")
			}
			index, err := strconv.Atoi(at.String())
			if err != nil || index < 0 {
				return nil, E.Extend(fault.ErrInvalid, "index must be a non-negative integer")
			}
			selector.index = &index
			continue
		}
		clause, err := parseClause(vocabulary, entry.key, entry.value)
		if err != nil {
			return nil, err
		}
		selector.scripted = selector.scripted || clause.relation != nil && clause.relation.scripted
		selector.clauses = append(selector.clauses, clause)
	}
	if len(selector.clauses) == 0 {
		return nil, E.Extend(fault.ErrInvalid, "the selector is empty")
	}
	return selector, nil
}

func parseClause[T comparable](vocabulary *Vocabulary[T], name string, value json.RawMessage) (clause[T], error) {
	clause := clause[T]{name: name}
	if slices.Contains(relations, name) {
		var element struct {
			Ref *string `json:"$element"`
		}
		if json.Unmarshal(value, &element) == nil && element.Ref != nil {
			clause.element = *element.Ref
			return clause, nil
		}
		decoder := json.NewDecoder(bytes.NewReader(value))
		decoder.UseNumber()
		relation, err := parseSelector(vocabulary, decoder)
		if err != nil {
			return clause, err
		}
		if name == "not" && relation.index != nil {
			return clause, E.Extend(fault.ErrInvalid, "the selector under not cannot have an index")
		}
		clause.relation = relation
		return clause, nil
	}
	index := slices.IndexFunc(vocabulary.Fields, func(field Field[T]) bool { return field.name == name })
	if index < 0 {
		names := slices.Concat(common.Map(vocabulary.Fields, func(field Field[T]) string { return field.name }), relations, []string{"index"})
		return clause, E.Extend(fault.ErrInvalid, "unknown element field ", strconv.Quote(name), "; the fields are ", strings.Join(names, ", "))
	}
	clause.field = vocabulary.Fields[index]
	switch {
	case clause.field.set != nil:
		if json.Unmarshal(value, &clause.set) != nil || clause.set == nil {
			return clause, E.Extend(fault.ErrInvalid, name, " must be an array of strings")
		}
	case clause.field.text != nil:
		match, err := parseText(vocabulary, name, value)
		if err != nil {
			return clause, err
		}
		clause.text = match
	default:
		var flag *bool
		if json.Unmarshal(value, &flag) != nil || flag == nil {
			return clause, E.Extend(fault.ErrInvalid, name, " must be a boolean")
		}
		clause.flag = *flag
	}
	return clause, nil
}

func parseText[T comparable](vocabulary *Vocabulary[T], name string, value json.RawMessage) (*textMatch, error) {
	var text *string
	if json.Unmarshal(value, &text) == nil && text != nil {
		return &textMatch{text: vocabulary.plainText(*text), script: -1}, nil
	}
	var pattern struct {
		Source *string `json:"$regex"`
		Flags  string  `json:"$flags"`
		ID     int     `json:"$id"`
	}
	if json.Unmarshal(value, &pattern) != nil || pattern.Source == nil {
		return nil, E.Extend(fault.ErrInvalid, name, " must be a string or a RegExp")
	}
	return &textMatch{regular: true, source: *pattern.Source, flags: pattern.Flags, script: pattern.ID}, nil
}

type objectEntry struct {
	key   string
	value json.RawMessage
}

type object []objectEntry

func (o object) number(key string) (int, bool) {
	for _, entry := range o {
		if entry.key == key {
			var number int
			if json.Unmarshal(entry.value, &number) == nil {
				return number, true
			}
		}
	}
	return 0, false
}

func readObject(decoder *json.Decoder, message string) (object, error) {
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, E.Extend(fault.ErrInvalid, message)
	}
	var out object
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, E.Extend(fault.ErrInvalid, message)
		}
		key, _ := token.(string)
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return nil, E.Extend(fault.ErrInvalid, message)
		}
		out = append(out, objectEntry{key: key, value: value})
	}
	if _, err = decoder.Token(); err != nil {
		return nil, E.Extend(fault.ErrInvalid, message)
	}
	return out, nil
}

func (s *Selector[T]) Scripted() bool {
	return s.scripted
}

func (s *Selector[T]) Fields() []string {
	var fields []string
	for _, clause := range s.clauses {
		if clause.relation != nil {
			fields = append(fields, clause.relation.Fields()...)
		} else {
			fields = append(fields, clause.name)
		}
	}
	return fields
}

func (s *Selector[T]) Exact(field string) string {
	for _, clause := range s.clauses {
		if clause.name == field && clause.text != nil && !clause.text.regular {
			return clause.text.text
		}
	}
	return ""
}

func (s *Selector[T]) Within() string {
	for _, clause := range s.clauses {
		if clause.name == "within" && clause.element != "" {
			return clause.element
		}
	}
	return ""
}

func (s *Selector[T]) String() string {
	if s.predicate >= 0 {
		return "the selector function"
	}
	parts := common.Map(s.clauses, clause[T].String)
	if s.index != nil {
		parts = append(parts, "index: "+strconv.Itoa(*s.index))
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

func (c clause[T]) String() string {
	switch {
	case c.text != nil && c.text.regular:
		return c.name + ": /" + c.text.source + "/" + c.text.flags
	case c.text != nil:
		return c.name + ": " + marshalText(c.text.text)
	case c.set != nil:
		return c.name + ": " + marshalText(c.set)
	case c.relation != nil:
		return c.name + ": " + c.relation.String()
	case c.element != "":
		return c.name + ": an element"
	}
	return c.name + ": " + strconv.FormatBool(c.flag)
}

func marshalText(value any) string {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(value)
	return strings.TrimSuffix(out.String(), "\n")
}

func (v *Vocabulary[T]) plainText(text string) string {
	if v.Plain == nil {
		return text
	}
	return v.Plain(text)
}

func (s *Selector[T]) Find(domain, universe []T, callbacks Callbacks[T]) (T, error) {
	var none T
	found, err := s.FindAll(domain, universe, callbacks)
	if err != nil || len(found) == 0 {
		return none, err
	}
	if len(found) > 1 {
		return none, E.New(s.String(), " is not unique: ", len(found), " elements match")
	}
	return found[0], nil
}

func (s *Selector[T]) FindAll(domain, universe []T, callbacks Callbacks[T]) ([]T, error) {
	match, err := s.matcher(universe, callbacks)
	if err != nil {
		return nil, err
	}
	var found []T
	matches := make(map[T]bool)
	for _, element := range domain {
		matched, err := match(element)
		if err != nil {
			return nil, err
		}
		if matched {
			found = append(found, element)
			matches[element] = true
		}
	}
	if s.vocabulary.Repeats != nil {
		found = slices.DeleteFunc(found, func(element T) bool { return s.repeatsAncestor(element, matches) })
	}
	if s.index == nil {
		return found, nil
	}
	if *s.index < len(found) {
		return found[*s.index : *s.index+1], nil
	}
	return nil, nil
}

func (s *Selector[T]) repeatsAncestor(element T, matches map[T]bool) bool {
	var none T
	for ancestor := s.vocabulary.Parent(element); ancestor != none; ancestor = s.vocabulary.Parent(ancestor) {
		if matches[ancestor] && s.vocabulary.Repeats(element, ancestor) {
			return true
		}
	}
	return false
}

func (s *Selector[T]) matcher(universe []T, callbacks Callbacks[T]) (func(T) (bool, error), error) {
	if s.predicate >= 0 {
		if callbacks == nil {
			return nil, ErrScripted
		}
		return func(element T) (bool, error) {
			return callbacks.Predicate(s.predicate, element)
		}, nil
	}
	checks := make([]func(T) (bool, error), 0, len(s.clauses))
	for _, clause := range s.clauses {
		check, err := s.check(clause, universe, callbacks)
		if err != nil {
			return nil, err
		}
		checks = append(checks, check)
	}
	return func(element T) (bool, error) {
		for _, check := range checks {
			matched, err := check(element)
			if err != nil || !matched {
				return false, err
			}
		}
		return true, nil
	}, nil
}

func (s *Selector[T]) check(clause clause[T], universe []T, callbacks Callbacks[T]) (func(T) (bool, error), error) {
	field := clause.field
	switch {
	case clause.name == "not" && clause.relation != nil:
		match, err := clause.relation.matcher(universe, callbacks)
		if err != nil {
			return nil, err
		}
		return func(element T) (bool, error) {
			matched, err := match(element)
			return !matched, err
		}, nil
	case clause.relation != nil || clause.element != "":
		scope, err := s.scope(clause, universe, callbacks)
		if err != nil {
			return nil, err
		}
		return func(element T) (bool, error) { return s.related(clause.name, element, scope), nil }, nil
	case field.set != nil:
		return func(element T) (bool, error) {
			have := field.set(element)
			return common.All(clause.set, func(item string) bool { return slices.Contains(have, item) }), nil
		}, nil
	case field.state != nil:
		return func(element T) (bool, error) {
			state := field.state(element)
			return state != nil && *state == clause.flag, nil
		}, nil
	case field.flag != nil:
		return func(element T) (bool, error) { return field.flag(element) == clause.flag, nil }, nil
	}
	return func(element T) (bool, error) {
		text, present := field.text(element)
		switch {
		case !present:
			return false, nil
		case !clause.text.regular:
			return s.vocabulary.plainText(text) == clause.text.text, nil
		case callbacks == nil:
			return false, ErrScripted
		}
		return callbacks.Match(clause.text.script, s.vocabulary.plainText(text))
	}, nil
}

func (s *Selector[T]) scope(clause clause[T], universe []T, callbacks Callbacks[T]) (map[T]bool, error) {
	var members []T
	if clause.element != "" {
		members = common.Filter(universe, func(element T) bool { return s.vocabulary.Ref(element) == clause.element })
	} else {
		var err error
		if members, err = clause.relation.FindAll(universe, universe, callbacks); err != nil {
			return nil, err
		}
	}
	scope := make(map[T]bool)
	var none T
	for _, member := range members {
		if clause.name != "has" {
			scope[member] = true
			continue
		}
		for ancestor := s.vocabulary.Parent(member); ancestor != none && !scope[ancestor]; ancestor = s.vocabulary.Parent(ancestor) {
			scope[ancestor] = true
		}
	}
	return scope, nil
}

func (s *Selector[T]) related(relation string, element T, scope map[T]bool) bool {
	var none T
	switch relation {
	case "parent":
		parent := s.vocabulary.Parent(element)
		return parent != none && scope[parent]
	case "has":
		return scope[element]
	case "not":
		return !scope[element]
	}
	for ancestor := s.vocabulary.Parent(element); ancestor != none; ancestor = s.vocabulary.Parent(ancestor) {
		if scope[ancestor] {
			return true
		}
	}
	return false
}
