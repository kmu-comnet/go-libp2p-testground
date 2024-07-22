package main

import (
	"github.com/testground/sdk-go/run"
)

var testcases = map[string]interface{}{
	"ten-text":         run.InitializedTestCaseFn(node),
	"fifty-smallimage": run.InitializedTestCaseFn(node),
	"hundred-bigimage": run.InitializedTestCaseFn(node),
}

func main() {
	run.InvokeMap(testcases)
}
