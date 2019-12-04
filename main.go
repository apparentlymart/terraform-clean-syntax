package main

import (
	"bytes"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	flag "github.com/spf13/pflag"
)

func main() {
	flag.Usage = func() {
		os.Stderr.WriteString("Usage: terraform-clean-syntax <dir>\n")
	}

	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	for _, arg := range args {
		processItem(arg)
	}
}

func processItem(fn string) {
	info, err := os.Lstat(fn)
	if err != nil {
		log.Printf("Failed to stat %q: %s\n", fn, err)
		return
	}

	if info.IsDir() {
		if strings.HasPrefix(info.Name(), ".") {
			return
		}
		processDir(fn)
	} else {
		if !info.Mode().IsRegular() {
			log.Printf("Skipping %q: not a regular file or directory", fn)
		}
		if !strings.HasSuffix(fn, ".tf") {
			return
		}
		processFile(fn, info.Mode())
	}
}

func processDir(fn string) {
	entries, err := ioutil.ReadDir(fn)
	if err != nil {
		log.Printf("Failed to read directory %q: %s", fn, err)
		return
	}

	for _, entry := range entries {
		processItem(filepath.Join(fn, entry.Name()))
	}
}

func processFile(fn string, mode os.FileMode) {
	src, err := ioutil.ReadFile(fn)
	if err != nil {
		log.Printf("Failed to read file %q: %s", fn, err)
		return
	}

	f, diags := hclwrite.ParseConfig(src, fn, hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		for _, diag := range diags {
			if diag.Subject != nil {
				log.Printf("[%s:%d] %s: %s", diag.Subject.Filename, diag.Subject.Start.Line, diag.Summary, diag.Detail)
			} else {
				log.Printf("%s: %s", diag.Summary, diag.Detail)
			}
		}
		return
	}

	cleanFile(f)

	newSrc := f.Bytes()
	if bytes.Equal(newSrc, src) {
		// No changes
		return
	}

	// TODO: Write the new file to disk in place of the old one
	err = ioutil.WriteFile(fn, newSrc, mode)
	if err != nil {
		log.Printf("Failed to write to %q: %s", fn, err)
		log.Printf("WARNING: File %q may be left with only partial content", fn)
		return
	}
	log.Printf("Made changes to %s", fn)
}

func cleanFile(f *hclwrite.File) {
	cleanBody(f.Body(), nil)
}

func cleanBody(body *hclwrite.Body, inBlocks []string) {
	attrs := body.Attributes()
	for name, attr := range attrs {
		if len(inBlocks) == 1 && inBlocks[0] == "variable" && name == "type" {
			cleanedExprTokens := cleanTypeExpr(attr.Expr().BuildTokens(nil))
			body.SetAttributeRaw(name, cleanedExprTokens)
			continue
		}
		cleanedExprTokens := cleanValueExpr(attr.Expr().BuildTokens(nil))
		body.SetAttributeRaw(name, cleanedExprTokens)
	}

	blocks := body.Blocks()
	for _, block := range blocks {
		inBlocks := append(inBlocks, block.Type())
		cleanBody(block.Body(), inBlocks)
	}
}

func cleanValueExpr(tokens hclwrite.Tokens) hclwrite.Tokens {
	if len(tokens) < 5 {
		// Can't possibly be a "${ ... }" sequence without at least enough
		// tokens for the delimiters and one token inside them.
		return tokens
	}
	oQuote := tokens[0]
	oBrace := tokens[1]
	cBrace := tokens[len(tokens)-2]
	cQuote := tokens[len(tokens)-1]
	if oQuote.Type != hclsyntax.TokenOQuote || oBrace.Type != hclsyntax.TokenTemplateInterp || cBrace.Type != hclsyntax.TokenTemplateSeqEnd || cQuote.Type != hclsyntax.TokenCQuote {
		// Not an interpolation sequence at all, then.
		return tokens
	}

	inside := tokens[2 : len(tokens)-2]

	// We're only interested in sequences that are provable to be single
	// interpolation sequences, which we'll determine by hunting inside
	// the interior tokens for any other interpolation sequences. This is
	// likely to produce false negatives sometimes, but that's better than
	// false positives and we're mainly interested in catching the easy cases
	// here.
	quotes := 0
	for _, token := range inside {
		if token.Type == hclsyntax.TokenOQuote {
			quotes++
			continue
		}
		if token.Type == hclsyntax.TokenCQuote {
			quotes--
			continue
		}
		if quotes > 0 {
			// Interpolation sequences inside nested quotes are okay, because
			// they are part of a nested expression.
			// "${foo("${bar}")}"
			continue
		}
		if token.Type == hclsyntax.TokenTemplateInterp || token.Type == hclsyntax.TokenTemplateSeqEnd {
			// We've found another template delimiter within our interior
			// tokens, which suggests that we've found something like this:
			// "${foo}${bar}"
			// That isn't unwrappable, so we'll leave the whole expression alone.
			return tokens
		}
	}

	// If we got down here without an early return then this looks like
	// an unwrappable sequence, but we'll trim any leading and trailing
	// newlines that might result in an invalid result if we were to
	// naively trim something like this:
	// "${
	//    foo
	// }"
	return trimNewlines(inside)
}

func cleanTypeExpr(tokens hclwrite.Tokens) hclwrite.Tokens {
	if len(tokens) != 3 {
		// We're only interested in plain quoted strings, which consist
		// of the open and close quotes and a literal string token.
		return tokens
	}
	oQuote := tokens[0]
	strTok := tokens[1]
	cQuote := tokens[2]
	if oQuote.Type != hclsyntax.TokenOQuote || strTok.Type != hclsyntax.TokenQuotedLit || cQuote.Type != hclsyntax.TokenCQuote {
		// Not a quoted string sequence, then.
		return tokens
	}

	switch string(strTok.Bytes) {
	case "string":
		return hclwrite.Tokens{
			{
				Type:  hclsyntax.TokenIdent,
				Bytes: []byte("string"),
			},
		}
	case "list":
		return hclwrite.Tokens{
			{
				Type:  hclsyntax.TokenIdent,
				Bytes: []byte("list"),
			},
			{
				Type:  hclsyntax.TokenOParen,
				Bytes: []byte("("),
			},
			{
				Type:  hclsyntax.TokenIdent,
				Bytes: []byte("string"),
			},
			{
				Type:  hclsyntax.TokenCParen,
				Bytes: []byte(")"),
			},
		}
	case "map":
		return hclwrite.Tokens{
			{
				Type:  hclsyntax.TokenIdent,
				Bytes: []byte("map"),
			},
			{
				Type:  hclsyntax.TokenOParen,
				Bytes: []byte("("),
			},
			{
				Type:  hclsyntax.TokenIdent,
				Bytes: []byte("string"),
			},
			{
				Type:  hclsyntax.TokenCParen,
				Bytes: []byte(")"),
			},
		}
	default:
		// Something else we're not expecting, then.
		return tokens
	}
}

func trimNewlines(tokens hclwrite.Tokens) hclwrite.Tokens {
	if len(tokens) == 0 {
		return nil
	}
	var start, end int
	for start = 0; start < len(tokens); start++ {
		if tokens[start].Type != hclsyntax.TokenNewline {
			break
		}
	}
	for end = len(tokens); end > 0; end-- {
		if tokens[end-1].Type != hclsyntax.TokenNewline {
			break
		}
	}
	return tokens[start:end]
}
