package main

import (
	"github.com/testground/sdk-go/run"
)

var testcases = map[string]interface{}{
	"ten-text":           run.InitializedTestCaseFn(node),
	"hundred-smallimage": run.InitializedTestCaseFn(node),
}

func main() {
	run.InvokeMap(testcases)
}
