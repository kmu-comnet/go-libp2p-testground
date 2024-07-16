package main

import (
	"github.com/testground/sdk-go/run"
)

var testcases = map[string]interface{}{
	"node": run.InitializedTestCaseFn(node),
}

func main() {
	run.InvokeMap(testcases)
}
