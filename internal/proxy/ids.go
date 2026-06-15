package proxy

import "sync"

const (
	OpenAIIDPrefix      = "chatcmpl-"
	OpenAICallPrefix    = "call_"
	AnthropicIDPrefix   = "msg_"
	AnthropicToolPrefix = "toolu_"
)

var thoughtSignatureCache sync.Map
