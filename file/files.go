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

// Package file includes file related manipulations.
package file

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/b3log/gulu"
	"github.com/b3log/wide/conf"
	"github.com/b3log/wide/event"
	"github.com/b3log/wide/session"
)

// Logger.
var logger = gulu.Log.NewLogger(os.Stdout)

// Node represents a file node in file tree.
type Node struct {
	Id        string  `json:"id"`
	Name      string  `json:"name"`
	Path      string  `json:"path"`
	IconSkin  string  `json:"iconSkin"` // Value should be end with a space
	IsParent  bool    `json:"isParent"`
	Type      string  `json:"type"`      // "f": file, "d": directory
	Creatable bool    `json:"creatable"` // whether can create file in this file node
	Removable bool    `json:"removable"` // whether can remove this file node
	IsGoAPI   bool    `json:"isGOAPI"`
	Mode      string  `json:"mode"`
	GitClone  bool    `json:"gitClone"` //是否允许GITClone
	GitRepo   bool    `json:"gitRepo"`  //是否GIT目录
	Pathtype  int     `json:"pathtype"`
	Children  []*Node `json:"children"`
}

// Snippet represents a source code snippet, used to as the result of "Find Usages", "Search".
type Snippet struct {
	Path     string   `json:"path"`     // file path
	Line     int      `json:"line"`     // line number
	Ch       int      `json:"ch"`       // column number
	Contents []string `json:"contents"` // lines nearby
}

var rootNode *Node
var pathNode *Node

// initAPINode builds the Go API file node.
func initGoRoot() {
	goRoot := gulu.Go.GetAPIPath()
	rootNode = &Node{Name: "Go API", Path: goRoot, IconSkin: "ico-ztree-dir-api ", Type: "d",
		Creatable: false, Removable: false, IsGoAPI: true, GitClone: false, GitRepo: false, Pathtype: 1, Children: []*Node{}}
	logger.Debugf("initGoRoot goRoot [%s] ", goRoot)
	walk(goRoot, goRoot, rootNode, false, false, true, 1)
}

func initGoPath() {
	goPath := gulu.Go.GetPathPath()
	pathNode = &Node{Name: "Go PATH", Path: goPath, IconSkin: "ico-ztree-dir-api ", Type: "d",
		Creatable: false, Removable: false, IsGoAPI: true, GitClone: false, GitRepo: false, Pathtype: 2, Children: []*Node{}}
	logger.Debugf("initGoPath goPath [%s] ", goPath)
	walk(goPath, goPath, pathNode, false, false, true, 2)
}

// GetFilesHandler handles request of constructing user workspace file tree.
//
// The Go API source code package also as a child node,
// so that users can easily view the Go API source code in file tree.
func GetFilesHandler(w http.ResponseWriter, r *http.Request) {
	httpSession, _ := session.HTTPSession.Get(r, session.CookieName)
	if httpSession.IsNew {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	uid := httpSession.Values["uid"].(string)
	pathtype := 0

	result := gulu.Ret.NewResult()
	defer gulu.Ret.RetGzResult(w, r, result)

	userWorkspace := conf.GetUserWorkspace(uid)
	workspaces := filepath.SplitList(userWorkspace)

	root := Node{Name: "root", Path: "", IconSkin: "ico-ztree-dir ", Type: "d", Pathtype: pathtype, IsParent: true, GitClone: true, GitRepo: false, Children: []*Node{}}

	if nil == rootNode { // lazy init
		initGoRoot()
	}

	if nil == pathNode { // lazy init
		initGoPath()
	}

	// workspace node process
	for _, workspace := range workspaces {
		workspacePath := workspace + conf.PathSeparator + "src"

		workspaceNode := Node{
			Id:        filepath.ToSlash(workspacePath), // jQuery API can't accept "\", so we convert it to "/"
			Name:      workspace[strings.LastIndex(workspace, conf.PathSeparator)+1:],
			Path:      filepath.ToSlash(workspacePath),
			IconSkin:  "ico-ztree-dir-workspace ",
			Type:      "d",
			Creatable: true,
			Removable: false,
			IsGoAPI:   false,
			GitClone:  true,
			GitRepo:   false,
			Pathtype:  pathtype,
			Children:  []*Node{}}

		walk(workspacePath, workspacePath, &workspaceNode, true, true, false, pathtype)

		// add workspace node
		root.Children = append(root.Children, &workspaceNode)
	}

	// add Go API node

	root.Children = append(root.Children, rootNode)
	root.Children = append(root.Children, pathNode)

	result.Data = root
}

// RefreshDirectoryHandler handles request of refresh a directory of file tree.
func RefreshDirectoryHandler(w http.ResponseWriter, r *http.Request) {
	httpSession, _ := session.HTTPSession.Get(r, session.CookieName)
	if httpSession.IsNew {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	uid := httpSession.Values["uid"].(string)
	r.ParseForm()
	pathValue, pathtype := GetPath(uid, r.FormValue("path"), r.FormValue("pathtype"))
	if !gulu.Go.IsAPI(pathValue) && !gulu.Go.IsPath(pathValue) && !session.CanAccess(uid, pathValue) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	gitPath := filepath.Join(pathValue, ".git")
	isGit := pathExists(gitPath)
	node := Node{Name: "root", Path: pathValue, IconSkin: "ico-ztree-dir ", Type: "d", Pathtype: pathtype, GitClone: false, GitRepo: isGit, Children: []*Node{}}

	walk(pathValue, pathValue, &node, true, true, false, pathtype)

	w.Header().Set("Content-Type", "application/json")
	data, err := json.Marshal(node.Children)
	if err != nil {
		logger.Error(err)
		return
	}

	w.Write(data)
}

// GetFileHandler handles request of opening file by editor.
func GetFileHandler(w http.ResponseWriter, r *http.Request) {
	httpSession, _ := session.HTTPSession.Get(r, session.CookieName)
	if httpSession.IsNew {
		http.Error(w, "Forbidden", http.StatusForbidden)

		return
	}
	uid := httpSession.Values["uid"].(string)

	result := gulu.Ret.NewResult()
	defer gulu.Ret.RetResult(w, r, result)

	var args map[string]interface{}

	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		logger.Error(err)
		result.Code = -1

		return
	}

	path, _ := GetPath(uid, args["path"].(string), fmt.Sprint(args["pathtype"]))

	if !gulu.Go.IsAPI(path) && !gulu.Go.IsPath(path) && !session.CanAccess(uid, path) {
		http.Error(w, "Forbidden", http.StatusForbidden)

		return
	}

	size := gulu.File.GetFileSize(path)
	if size > 5242880 { // 5M
		result.Code = -1
		result.Msg = "This file is too large to open :("

		return
	}

	data := map[string]interface{}{}
	result.Data = &data

	buf, _ := ioutil.ReadFile(path)

	extension := filepath.Ext(path)

	if gulu.File.IsImg(extension) {
		// image file will be open in a browser tab

		data["mode"] = "img"

		userId := conf.GetOwner(path)
		if "" == userId {
			logger.Warnf("The path [%s] has no owner", path)
			data["path"] = ""

			return
		}

		user := conf.GetUser(uid)

		data["path"] = "/workspace/" + user.Name + "/" + strings.Replace(path, user.WorkspacePath(), "", 1)

		return
	}

	content := string(buf)

	if gulu.File.IsBinary(content) {
		result.Code = -1
		result.Msg = "Can't open a binary file :("
	} else {
		data["content"] = content
		data["path"] = path
	}
}

// SaveFileHandler handles request of saving file.
func SaveFileHandler(w http.ResponseWriter, r *http.Request) {
	httpSession, _ := session.HTTPSession.Get(r, session.CookieName)
	if httpSession.IsNew {
		http.Error(w, "Forbidden", http.StatusForbidden)

		return
	}
	uid := httpSession.Values["uid"].(string)

	result := gulu.Ret.NewResult()
	defer gulu.Ret.RetResult(w, r, result)

	var args map[string]interface{}

	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		logger.Error(err)
		result.Code = -1

		return
	}

	// filePath := args["file"].(string)
	sid := args["sid"].(string)

	filePath, _ := GetPath(uid, args["file"].(string), fmt.Sprint(args["pathtype"]))

	if gulu.Go.IsAPI(filePath) || gulu.Go.IsPath(filePath) || !session.CanAccess(uid, filePath) {
		http.Error(w, "Forbidden", http.StatusForbidden)

		return
	}

	fout, err := os.Create(filePath)

	if nil != err {
		logger.Error(err)
		result.Code = -1

		return
	}

	code := args["code"].(string)

	fout.WriteString(code)

	if err := fout.Close(); nil != err {
		logger.Error(err)
		result.Code = -1

		wSession := session.WideSessions.Get(sid)
		wSession.EventQueue.Queue <- &event.Event{Code: event.EvtCodeServerInternalError, Sid: sid,
			Data: "can't save file " + filePath}

		return
	}
}

// NewFileHandler handles request of creating file or directory.
func NewFileHandler(w http.ResponseWriter, r *http.Request) {
	httpSession, _ := session.HTTPSession.Get(r, session.CookieName)
	if httpSession.IsNew {
		http.Error(w, "Forbidden", http.StatusForbidden)

		return
	}
	uid := httpSession.Values["uid"].(string)

	result := gulu.Ret.NewResult()
	defer gulu.Ret.RetResult(w, r, result)

	var args map[string]interface{}

	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		logger.Error(err)
		result.Code = -1
		return
	}

	path, _ := GetPath(uid, args["path"].(string), fmt.Sprint(args["pathtype"]))

	if gulu.Go.IsAPI(path) || gulu.Go.IsPath(path) || !session.CanAccess(uid, path) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	fileType := args["fileType"].(string)
	sid := args["sid"].(string)

	wSession := session.WideSessions.Get(sid)

	if !createFile(path, fileType) {
		result.Code = -1

		wSession.EventQueue.Queue <- &event.Event{Code: event.EvtCodeServerInternalError, Sid: sid,
			Data: "can't create file " + path}

		return
	}

	if "f" == fileType {
		logger.Debugf("Created a file [%s] by user [%s]", path, wSession.UserId)
	} else {
		logger.Debugf("Created a dir [%s] by user [%s]", path, wSession.UserId)
	}

}

// RemoveFileHandler handles request of removing file or directory.
func RemoveFileHandler(w http.ResponseWriter, r *http.Request) {
	httpSession, _ := session.HTTPSession.Get(r, session.CookieName)
	if httpSession.IsNew {
		http.Error(w, "Forbidden", http.StatusForbidden)

		return
	}
	uid := httpSession.Values["uid"].(string)

	result := gulu.Ret.NewResult()
	defer gulu.Ret.RetResult(w, r, result)

	var args map[string]interface{}

	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		logger.Error(err)
		result.Code = -1
		return
	}

	// path := args["path"].(string)
	path, _ := GetPath(uid, args["path"].(string), fmt.Sprint(args["pathtype"]))

	if gulu.Go.IsAPI(path) || gulu.Go.IsPath(path) || !session.CanAccess(uid, path) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	sid := args["sid"].(string)

	wSession := session.WideSessions.Get(sid)

	if !removeFile(path) {
		result.Code = -1

		wSession.EventQueue.Queue <- &event.Event{Code: event.EvtCodeServerInternalError, Sid: sid,
			Data: "can't remove file " + path}

		return
	}

	logger.Debugf("Removed a file [%s] by user [%s]", path, wSession.UserId)
}

// RenameFileHandler handles request of renaming file or directory.
func RenameFileHandler(w http.ResponseWriter, r *http.Request) {
	httpSession, _ := session.HTTPSession.Get(r, session.CookieName)
	if httpSession.IsNew {
		http.Error(w, "Forbidden", http.StatusForbidden)

		return
	}
	uid := httpSession.Values["uid"].(string)

	result := gulu.Ret.NewResult()
	defer gulu.Ret.RetResult(w, r, result)

	var args map[string]interface{}

	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		logger.Error(err)
		result.Code = -1

		return
	}

	oldPath, _ := GetPath(uid, args["oldPath"].(string), fmt.Sprint(args["pathtype"]))
	newPath, _ := GetPath(uid, args["newPath"].(string), fmt.Sprint(args["pathtype"]))
	// oldPath := args["oldPath"].(string)
	// newPath := args["newPath"].(string)
	if gulu.Go.IsAPI(oldPath) || gulu.Go.IsPath(oldPath) ||
		!session.CanAccess(uid, oldPath) {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	if gulu.Go.IsAPI(newPath) || gulu.Go.IsPath(newPath) || !session.CanAccess(uid, newPath) {
		http.Error(w, "Forbidden", http.StatusForbidden)

		return
	}

	sid := args["sid"].(string)

	wSession := session.WideSessions.Get(sid)

	if !renameFile(oldPath, newPath) {
		result.Code = -1

		wSession.EventQueue.Queue <- &event.Event{Code: event.EvtCodeServerInternalError, Sid: sid,
			Data: "can't rename file " + oldPath}

		return
	}

	logger.Debugf("Renamed a file [%s] to [%s] by user [%s]", oldPath, newPath, wSession.UserId)
}

// Use to find results sorting.
type foundPath struct {
	Path     string `json:"path"`
	score    int
	pathtype int
}

type foundPaths []*foundPath

func (f foundPaths) Len() int           { return len(f) }
func (f foundPaths) Swap(i, j int)      { f[i], f[j] = f[j], f[i] }
func (f foundPaths) Less(i, j int) bool { return f[i].score > f[j].score }

// FindHandler handles request of find files under the specified directory with the specified filename pattern.
func FindHandler(w http.ResponseWriter, r *http.Request) {
	httpSession, _ := session.HTTPSession.Get(r, session.CookieName)
	if httpSession.IsNew {
		http.Error(w, "Forbidden", http.StatusForbidden)

		return
	}
	uid := httpSession.Values["uid"].(string)

	result := gulu.Ret.NewResult()
	defer gulu.Ret.RetResult(w, r, result)

	var args map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		logger.Error(err)
		result.Code = -1

		return
	}

	// path := args["path"].(string) // path of selected file in file tree
	path, _ := GetPath(uid, args["path"].(string), fmt.Sprint(args["pathtype"]))

	if !gulu.Go.IsAPI(path) && !gulu.Go.IsPath(path) && !session.CanAccess(uid, path) {
		http.Error(w, "Forbidden", http.StatusForbidden)

		return
	}

	name := args["name"].(string)

	userWorkspace := conf.GetUserWorkspace(uid)
	workspaces := filepath.SplitList(userWorkspace)

	if "" != path && !gulu.File.IsDir(path) {
		path = filepath.Dir(path)
	}

	founds := foundPaths{}

	for _, workspace := range workspaces {
		rs := find(workspace+conf.PathSeparator+"src", name, []*string{})

		for _, r := range rs {
			substr := gulu.Str.LCS(path, *r)

			founds = append(founds, &foundPath{Path: filepath.ToSlash(*r), score: len(substr)})
		}
	}

	sort.Sort(founds)

	result.Data = founds
}

// SearchTextHandler handles request of searching files under the specified directory with the specified keyword.
func SearchTextHandler(w http.ResponseWriter, r *http.Request) {
	httpSession, _ := session.HTTPSession.Get(r, session.CookieName)
	if httpSession.IsNew {
		http.Error(w, "Forbidden", http.StatusForbidden)

		return
	}

	result := gulu.Ret.NewResult()
	defer gulu.Ret.RetResult(w, r, result)

	var args map[string]interface{}

	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		logger.Error(err)
		result.Code = -1

		return
	}

	sid := args["sid"].(string)
	wSession := session.WideSessions.Get(sid)
	if nil == wSession {
		result.Code = -1

		return
	}

	// XXX: just one directory

	// dir := args["dir"].(string)
	dir, _ := GetPath(sid, args["dir"].(string), fmt.Sprint(args["pathtype"]))
	if "" == dir {
		userWorkspace := conf.GetUserWorkspace(wSession.UserId)
		workspaces := filepath.SplitList(userWorkspace)
		dir = workspaces[0]
	}

	extension := args["extension"].(string)
	text := args["text"].(string)

	founds := []*Snippet{}
	if gulu.File.IsDir(dir) {
		founds = search(dir, extension, text, []*Snippet{})
	} else {
		founds = searchInFile(dir, text)
	}

	result.Data = founds
}

// walk traverses the specified path to build a file tree.
func walk(path, rootpath string, node *Node, creatable, removable, isGOAPI bool, pathtype int) {
	files := listFiles(path)

	for _, filename := range files {
		fpath := filepath.Join(path, filename)

		fio, _ := os.Lstat(fpath)

		child := Node{
			Id:        filepath.ToSlash(fpath)[len(rootpath):], // jQuery API can't accept "\", so we convert it to "/"
			Name:      filename,
			Path:      filepath.ToSlash(fpath)[len(rootpath):],
			Removable: removable,
			IsGoAPI:   isGOAPI,
			Pathtype:  pathtype,
			Children:  []*Node{}}
		node.Children = append(node.Children, &child)

		if nil == fio {
			logger.Warnf("Path [%s] is nil", fpath)
			continue
		}

		if fio.IsDir() {
			child.Type = "d"
			child.Creatable = creatable
			child.IconSkin = "ico-ztree-dir "
			child.IsParent = true
			child.GitClone = false
			gitPath := filepath.Join(fpath, ".git")
			child.GitRepo = pathExists(gitPath)

			walk(fpath, rootpath, &child, creatable, removable, isGOAPI, pathtype)
		} else {
			child.Type = "f"
			child.Creatable = creatable
			ext := filepath.Ext(fpath)

			child.IconSkin = getIconSkin(ext)
		}
	}

	return
}

func GetPath(uid, pathValue, pathtype string) (string, int) {
	logger.Debugf("User [%s] getPath pathtype:[%s] getPath [%s] ", uid, pathtype, pathValue)
	if pathtype == "0" {
		userWorkspace := conf.GetUserWorkspace(uid)
		workspaces := filepath.SplitList(userWorkspace)
		if len(workspaces) > 0 {
			path := filepath.Join(workspaces[0]+conf.PathSeparator, "src")
			path = filepath.Join(path, pathValue)
			pathValue = filepath.ToSlash(path)
			logger.Debugf("User [%s] pathtype:[%s] getPath [%s] ", uid, pathtype, pathValue)
			return pathValue, 0
		}
	} else if pathtype == "1" {
		pathValue = filepath.Join(gulu.Go.GetAPIPath(), pathValue)
		pathValue = filepath.ToSlash(pathValue)
		logger.Debugf("User [%s] pathtype:[%s] getPath [%s] ", uid, pathtype, pathValue)
		return pathValue, 1
	} else if pathtype == "2" {
		pathValue = filepath.Join(gulu.Go.GetPathPath(), pathValue)
		pathValue = filepath.ToSlash(pathValue)
		logger.Debugf("User [%s] pathtype:[%s] getPath [%s] ", uid, pathtype, pathValue)
		return pathValue, 2
	}

	logger.Debugf("User [%s] pathtype:[%s] getPath [%s] ", uid, "-1", "")
	return "", -1
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	return false
}

// listFiles lists names of files under the specified dirname.
func listFiles(dirname string) []string {
	f, _ := os.Open(dirname)

	names, _ := f.Readdirnames(-1)
	f.Close()

	sort.Strings(names)

	dirs := []string{}
	files := []string{}

	// sort: directories in front of files
	for _, name := range names {
		path := filepath.Join(dirname, name)
		fio, err := os.Lstat(path)

		if nil != err {
			logger.Warnf("Can't read file info [%s]", path)

			continue
		}

		if fio.IsDir() {
			// exclude the .git, .svn, .hg direcitory
			if ".git" == fio.Name() || ".svn" == fio.Name() || ".hg" == fio.Name() {
				continue
			}

			dirs = append(dirs, name)
		} else {
			// exclude the .DS_Store directory on Mac OS X
			if ".DS_Store" == fio.Name() {
				continue
			}

			files = append(files, name)
		}
	}

	return append(dirs, files...)
}

// getIconSkin gets CSS class name of icon with the specified filename extension.
//
// Refers to the zTree document for CSS class names.
func getIconSkin(filenameExtension string) string {
	if gulu.File.IsImg(filenameExtension) {
		return "ico-ztree-img "
	}

	switch filenameExtension {
	case ".html", ".htm":
		return "ico-ztree-html "
	case ".go":
		return "ico-ztree-go "
	case ".css":
		return "ico-ztree-css "
	case ".txt":
		return "ico-ztree-text "
	case ".sql":
		return "ico-ztree-sql "
	case ".properties":
		return "ico-ztree-pro "
	case ".md":
		return "ico-ztree-md "
	case ".js", ".json":
		return "ico-ztree-js "
	case ".xml":
		return "ico-ztree-xml "
	default:
		return "ico-ztree-other "
	}
}

// createFile creates file on the specified path.
//
// fileType:
//
//  "f": file
//  "d": directory
func createFile(path, fileType string) bool {
	switch fileType {
	case "f":
		file, err := os.OpenFile(path, os.O_CREATE, 0775)
		if nil != err {
			logger.Error(err)

			return false
		}

		defer file.Close()

		logger.Tracef("Created file [%s]", path)

		return true
	case "d":
		err := os.Mkdir(path, 0775)

		if nil != err {
			logger.Error(err)

			return false
		}

		logger.Tracef("Created directory [%s]", path)

		return true
	default:
		logger.Errorf("Unsupported file type [%s]", fileType)

		return false
	}
}

// removeFile removes file on the specified path.
func removeFile(path string) bool {
	if err := os.RemoveAll(path); nil != err {
		logger.Errorf("Removes [%s] failed: [%s]", path, err.Error())

		return false
	}

	logger.Tracef("Removed [%s]", path)

	return true
}

// renameFile renames (moves) a file from the specified old path to the specified new path.
func renameFile(oldPath, newPath string) bool {
	if err := os.Rename(oldPath, newPath); nil != err {
		logger.Errorf("Renames [%s] failed: [%s]", oldPath, err.Error())

		return false
	}

	logger.Tracef("Renamed [%s] to [%s]", oldPath, newPath)

	return true
}

// Default exclude file name patterns when find.
var defaultExcludesFind = []string{".git", ".svn", ".repository", "CVS", "RCS", "SCCS", ".bzr", ".metadata", ".hg"}

// find finds files under the specified dir and its sub-directoryies with the specified name,
// likes the command 'find dir -name name'.
func find(dir, name string, results []*string) []*string {
	if !strings.HasSuffix(dir, conf.PathSeparator) {
		dir += conf.PathSeparator
	}

	f, _ := os.Open(dir)
	fileInfos, err := f.Readdir(-1)
	f.Close()

	if nil != err {
		logger.Errorf("Read dir [%s] failed: [%s]", dir, err.Error())

		return results
	}

	for _, fileInfo := range fileInfos {
		fname := fileInfo.Name()
		path := dir + fname

		if fileInfo.IsDir() {
			if gulu.Str.Contains(fname, defaultExcludesFind) {
				continue
			}

			// enter the directory recursively
			results = find(path, name, results)
		} else {
			// match filename
			pattern := filepath.Dir(path) + conf.PathSeparator + name

			match, err := filepath.Match(strings.ToLower(pattern), strings.ToLower(path))

			if nil != err {
				logger.Errorf("Find match filename failed: [%s]", err.Error())

				continue
			}

			if match {
				results = append(results, &path)
			}
		}
	}

	return results
}

// search finds file under the specified dir and its sub-directories with the specified text, likes the command 'grep'
// or 'findstr'.
func search(dir, extension, text string, snippets []*Snippet) []*Snippet {
	if !strings.HasSuffix(dir, conf.PathSeparator) {
		dir += conf.PathSeparator
	}

	f, _ := os.Open(dir)
	fileInfos, err := f.Readdir(-1)
	f.Close()

	if nil != err {
		logger.Errorf("Read dir [%s] failed: [%s]", dir, err.Error())

		return snippets
	}

	for _, fileInfo := range fileInfos {
		path := dir + fileInfo.Name()

		if fileInfo.IsDir() {
			// enter the directory recursively
			snippets = search(path, extension, text, snippets)
		} else if strings.HasSuffix(path, extension) {
			// grep in file
			ss := searchInFile(path, text)

			snippets = append(snippets, ss...)
		}
	}

	return snippets
}

// searchInFile finds file with the specified path and text.
func searchInFile(path string, text string) []*Snippet {
	ret := []*Snippet{}

	bytes, err := ioutil.ReadFile(path)
	if nil != err {
		logger.Errorf("Read file [%s] failed: [%s]", path, err.Error())

		return ret
	}

	content := string(bytes)
	if gulu.File.IsBinary(content) {
		return ret
	}

	lines := strings.Split(content, "\n")

	for idx, line := range lines {
		ch := strings.Index(strings.ToLower(line), strings.ToLower(text))

		if -1 != ch {
			snippet := &Snippet{Path: filepath.ToSlash(path),
				Line: idx + 1, Ch: ch + 1, Contents: []string{line}}

			ret = append(ret, snippet)
		}
	}

	return ret
}
