package main

import (
	"github.com/testground/sdk-go/run"
)

var testcases = map[string]interface{}{
	"textfile":   run.InitializedTestCaseFn(node),
	"smallimage": run.InitializedTestCaseFn(node),
}

// Testground run이 어떤 test case를 지정받아 실행되었는지에 따라, 프로그램의 어떤 함수를 호출할 것인지를 map해 놓은 메인 함수입니다. 
// 현재는 모든 case에서 node라는 단일 함수를 실행하고 있지만, 필요에 따라 확장이 가능합니다. 
// Key에 해당하는 부분은 manifest.toml 파일에 명세한 내용과 일치해야 합니다.

func main() {
	run.InvokeMap(testcases)
}
