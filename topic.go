package main

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	HIDDEN      = '$'
	WILD_SINGLE = '+' // U+002B
	WILD_MULTI  = '#' // U+0023

	TOKEN_SEP    = "(?:/|$)"
	TOKEN_SINGLE = "([\\d\\w\\s-_]+(?:/|$))"
	TOKEN_MULTI  = "((?:[\\d\\w\\s-_]+/?)*)$"
)

type Parser struct {
	topic       string
	hidden, sys bool
	tokens      []string
	params      []Param
	re          *regexp.Regexp
}

type Param struct {
	i, index int
	wild     rune
	re       string
}

func NewParser(topic string) *Parser {
	return &Parser{topic: topic}
}

func (p *Parser) Match(topic string) bool {
	return p.re.MatchString(topic)
}

func (p *Parser) Params(topic string) []string {
	return p.re.FindStringSubmatch(topic)[1:]
}

func (p *Parser) Parse() {
	subtopic := []rune(p.topic)
	index := 0

	p.tokens = strings.Split(p.topic, "/")
	p.hidden = p.topic[0] == HIDDEN
	p.sys = p.tokens[0] == "$SYS"

	for i, token := range p.tokens {
		idx := strings.Index(string(subtopic), token)
		subtopic = subtopic[idx:]
		index += idx

		param := Param{i: i, index: index}

		switch token {
		case "+":
			param.wild = WILD_SINGLE
			param.re = TOKEN_SINGLE
		case "#":
			param.wild = WILD_MULTI
			param.re = TOKEN_MULTI
		default:
			continue
		}

		p.params = append(p.params, param)
		p.tokens[i] = param.re
	}

	fmt.Println(p.tokens)

	p.re = regexp.MustCompile(strings.Join(p.tokens, "/"))
}

func Parse(topic string) *Parser {
	parser := NewParser(topic)
	parser.Parse()
	return parser
}
