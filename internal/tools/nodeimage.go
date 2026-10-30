package tools

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// nodeImageConst names the constant in rooket's cmd/nodeimage.go that holds
// its default kind node image.
const nodeImageConst = "defaultNodeImage"

// NodeImageTag returns the tag of the kind node image rooket boots by
// default, read from the rooket module source in rooketModuleDir: the tag of
// the string constant defaultNodeImage in cmd/nodeimage.go, for example
// v1.33.1 from kindest/node:v1.33.1@sha256:….
func NodeImageTag(rooketModuleDir string) (string, error) {
	file := filepath.Join(rooketModuleDir, "cmd", "nodeimage.go")
	f, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.SkipObjectResolution)
	if err != nil {
		return "", fmt.Errorf("reading rooket's %s: %w", nodeImageConst, err)
	}
	ref, err := stringConst(f, nodeImageConst)
	if err != nil {
		return "", fmt.Errorf("reading rooket's %s from %s: %w", nodeImageConst, file, err)
	}
	tag, err := imageTag(ref)
	if err != nil {
		return "", fmt.Errorf("reading rooket's %s from %s: %w", nodeImageConst, file, err)
	}
	return tag, nil
}

// stringConst returns the value of the top-level constant name in f, which
// must be declared with a string literal.
func stringConst(f *ast.File, name string) (string, error) {
	for _, decl := range f.Decls {
		gd, ok := decl.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, spec := range gd.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			i := slices.IndexFunc(vs.Names, func(id *ast.Ident) bool { return id.Name == name })
			if i < 0 {
				continue
			}
			if i < len(vs.Values) {
				if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
					return strconv.Unquote(lit.Value)
				}
			}
			return "", errors.New("not a string literal")
		}
	}
	return "", errors.New("no such constant")
}

// imageTag returns the tag of the image reference ref, ignoring a digest. A
// colon before the last slash separates a registry's port, not a tag.
func imageTag(ref string) (string, error) {
	name, _, _ := strings.Cut(ref, "@")
	i := strings.LastIndexAny(name, ":/")
	if i < 0 || name[i] != ':' || i == len(name)-1 {
		return "", fmt.Errorf("image %q has no tag", ref)
	}
	return name[i+1:], nil
}
