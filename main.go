package main

import (
	"github.com/testground/sdk-go/run"
)

var testcases = map[string]interface{}{
	"textfile":   run.InitializedTestCaseFn(node),
	"smallimage": run.InitializedTestCaseFn(node),
}

func main() {
	run.InvokeMap(testcases)
}
