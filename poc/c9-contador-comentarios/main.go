package main

import (
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

func isBuildTag(text string) bool {
	return strings.HasPrefix(text, "//go:build")
}

func main() {
	fset := token.NewFileSet()
	total := 0
	files := 0
	failing := 0

	for _, root := range os.Args[1:] {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			files++
			file, perr := parser.ParseFile(fset, path, nil, parser.ParseComments)
			if perr != nil {
				failing++
				fmt.Printf("PARSE_ERROR\t%s\t%v\n", path, perr)
				return nil
			}
			count := 0
			for _, group := range file.Comments {
				if isBuildTag(group.List[0].Text) {
					continue
				}
				count++
				fmt.Printf("VIOLACION\t%s:%d\t%s\n", path, fset.Position(group.Pos()).Line, group.List[0].Text)
			}
			total += count
			if count > 0 {
				failing++
			}
			fmt.Printf("OK\t%d\t%s\n", count, path)
			return nil
		})
		if err != nil {
			fmt.Printf("WALK_ERROR\t%s\t%v\n", root, err)
			os.Exit(2)
		}
	}

	fmt.Printf("TOTAL\tarchivos=%d\tcomentarios_no_build_tag=%d\tarchivos_con_violacion=%d\n", files, total, failing)
	if total > 0 {
		os.Exit(1)
	}
}