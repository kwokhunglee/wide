// Copyright (c) 2014-present, b3log.org
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os/exec"
	"path/filepath"

	"github.com/kwokhunglee/wide/conf"
	"github.com/kwokhunglee/wide/file"
	"github.com/kwokhunglee/wide/gulu"
	"github.com/kwokhunglee/wide/i18n"
	"github.com/kwokhunglee/wide/session"
)

// GoTestHandler handles request of go test.
func GoTestHandler(w http.ResponseWriter, r *http.Request) {
	result := gulu.Ret.NewResult()
	defer gulu.Ret.RetResult(w, r, result)

	httpSession, _ := session.HTTPSession.Get(r, session.CookieName)
	if httpSession.IsNew {
		http.Error(w, "Forbidden", http.StatusForbidden)

		return
	}
	uid := httpSession.Values["uid"].(string)
	locale := conf.GetUser(uid).Locale

	var args map[string]interface{}

	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		logger.Error(err)
		result.Code = -1

		return
	}

	sid := args["sid"].(string)

	// filePath := args["file"].(string)
	filePath, _ := file.GetPath(uid, args["file"].(string), fmt.Sprint(args["pathtype"]))
	curDir := filepath.Dir(filePath)

	cmd := exec.Command("go", "test", "-v")
	cmd.Dir = curDir

	setCmdEnv(cmd, uid)

	stdout, err := cmd.StdoutPipe()
	if nil != err {
		logger.Error(err)
		result.Code = -1

		return
	}

	stderr, err := cmd.StderrPipe()
	if nil != err {
		logger.Error(err)
		result.Code = -1

		return
	}

	if 0 != result.Code {
		return
	}

	channelRet := map[string]interface{}{}

	if nil != session.OutputWS[sid] {
		// display "START [go test]" in front-end browser

		channelRet["output"] = "<span class='start-test'>" + i18n.Get(locale, "start-test").(string) + "</span>\n"
		channelRet["cmd"] = "start-test"

		wsChannel := session.OutputWS[sid]

		err := wsChannel.WriteJSON(&channelRet)
		if nil != err {
			logger.Warn(err)
			return
		}

		wsChannel.Refresh()
	}

	reader := bufio.NewReader(io.MultiReader(stdout, stderr))

	if err := cmd.Start(); nil != err {
		logger.Error(err)
		result.Code = -1

		return
	}

	go func(runningId int) {
		defer gulu.Panic.Recover(nil)

		logger.Debugf("User [%s, %s] is running [go test] [runningId=%d]", uid, sid, runningId)

		channelRet := map[string]interface{}{}
		channelRet["cmd"] = "go test"

		// read all
		buf, _ := ioutil.ReadAll(reader)

		// waiting for go test finished
		cmd.Wait()

		if !cmd.ProcessState.Success() {
			logger.Debugf("User [%s, %s] 's running [go test] [runningId=%d] has done (with error)", uid, sid, runningId)

			channelRet["output"] = "<span class='test-error'>" + i18n.Get(locale, "test-error").(string) + "</span>\n" + string(buf)
		} else {
			logger.Debugf("User [%s, %s] 's running [go test] [runningId=%d] has done", uid, sid, runningId)

			channelRet["output"] = "<span class='test-succ'>" + i18n.Get(locale, "test-succ").(string) + "</span>\n" + string(buf)
		}

		if nil != session.OutputWS[sid] {
			wsChannel := session.OutputWS[sid]

			err := wsChannel.WriteJSON(&channelRet)
			if nil != err {
				logger.Warn(err)
			}

			wsChannel.Refresh()
		}
	}(rand.Int())
}
