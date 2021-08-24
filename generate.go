// +build ignore

package main

import (
	"fmt"
	"io/ioutil"
	"regexp"
	"strings"
)

//go:generate go run github.com/99designs/gqlgen run
//go:generate go run generate.go

//func main() {
//	genOperationNames()
//}

func genOperationNames() {
	var operationRe = regexp.MustCompile(`type (Query|Mutation|Subscription).*{\s+(\w+)`)

	data, err := ioutil.ReadFile("./schema.graphqls")
	if err != nil {
		panic(err)
	}

	var variables []string

	for _, tp := range operationRe.FindAllStringSubmatch(string(data), -1) {
		variables = append(variables, fmt.Sprintf("%s Operation = \"%s\"", strings.Title(tp[2]), tp[2]))
	}

	var formattedVariables strings.Builder

	formattedVariables.WriteString("const (\n")

	for _, variable := range variables {
		formattedVariables.WriteString("	" + variable)
		formattedVariables.WriteString("\n")
	}

	formattedVariables.WriteString("\n)")

	formatted := fmt.Sprintf("package graph\n\ntype Operation string\n\n%s", formattedVariables.String())
	ioutil.WriteFile("./graph/operations.go", []byte(formatted), 0644)
}
