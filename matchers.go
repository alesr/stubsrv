package stubsrv

import (
	"net/url"
	"strings"
)

func pathMatch(tplSegs []string, rawPath string) bool {
	reqSegs := strings.Split(strings.Trim(rawPath, "/"), "/")
	if len(reqSegs) != len(tplSegs) {
		return false
	}
	for i, seg := range tplSegs {
		if strings.HasPrefix(seg, ":") {
			continue
		}
		if seg != reqSegs[i] {
			return false
		}
	}
	return true
}

func queryMatch(tpl map[string]string, urlVals url.Values) bool {
	if len(tpl) == 0 {
		return true
	}
	for k, v := range tpl {
		if urlVals.Get(k) != v {
			return false
		}
	}
	return true
}
