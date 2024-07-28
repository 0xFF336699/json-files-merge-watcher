package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/fsnotify/fsnotify"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"
)

type WatchList struct {
	Files   []string
	Folders []string
}

const (
	FolderTypeLocales     = "locales"
	FolderTypeSingle      = "single" // default
	KeyTypeJoinFolderFile = "joinFolderFile"
	KeyTypeOriginal       = "original" // default
)

type WatchData struct {
	FolderType        string
	KeyType           string
	Name              string
	Output            string
	TsInterfaceOutput string
	TsInterfaceName   string
	Suffix            string
	KeyOutput         string
	KeyValueName      string
	KeyFlatten        bool
	WatchList         []*WatchList
	finish            chan bool
	index             int
	terminted         bool
	timer             *time.Timer
	watcher           *fsnotify.Watcher
	children          []*WatchData
}

func (w *WatchData) onChildChange() {
	if w.children == nil {
		return
	}

}
func (d *WatchData) later() {
	t := d.timer
	if t != nil {
		t.Stop()
		println("cancel", d.Name, d.index)
	}
	t = time.NewTimer(time.Duration(conf.Delay * time.Microsecond))
	d.timer = t
	for {
		<-t.C
		t.Stop()
		d.timer = nil
		mergeGroup(d)
		return
	}
}

type Conf struct {
	Delay time.Duration
	List  []*WatchData
}

var conf Conf

var watchDataIndex = 0
var confName = "config.json"

func main() {
	run()
}

func run() {
	go watchConfig()
	go load()
	select {}
}

func load() {
	configBytes, err := ioutil.ReadFile(confName)
	if err != nil {
		println("read config file error", err.Error(), "wait for config change next time")
		go reload()
		return
	}
	err = json.Unmarshal(configBytes, &conf)
	if err != nil {
		println("unmarshal config file error", err.Error(), "wait for config change next time", string(configBytes))
		go reload()
		return
	}
	for _, d := range conf.List {
		switch d.FolderType {
		case FolderTypeLocales:
			runWatchLocales(d)
			break
		case FolderTypeSingle:
			runWatchData(d)
			break
		default:
			runWatchData(d)
		}
		//d.terminted = false
		//d.finish = make(chan bool)
		//watchDataIndex++
		//d.index = watchDataIndex
		//go watch(d)
	}
}

func runWatchLocales(d *WatchData) {
	files, _ := ioutil.ReadDir(d.WatchList[0].Folders[0])
	for i := 0; i < len(files); i++ {
		f := files[i]
		if !f.IsDir() {
			continue
		}
		output := path.Join(d.Output, f.Name()+".json")
		ts := path.Join(d.Output, f.Name()+".i18n.interface.ts")
		watchList := &WatchList{
			Files:   []string{},
			Folders: []string{path.Join(d.Output, f.Name())},
		}
		data := &WatchData{
			FolderType:        "",
			Name:              "",
			Output:            output,
			TsInterfaceOutput: ts,
			TsInterfaceName:   "II18n" + f.Name(),
			Suffix:            ".json",
			WatchList:         []*WatchList{watchList},
			finish:            nil,
			index:             0,
			terminted:         false,
			timer:             nil,
			watcher:           nil,
		}
		runWatchData(data)
	}
}
func runWatchData(d *WatchData) {
	d.terminted = false
	d.finish = make(chan bool)
	watchDataIndex++
	d.index = watchDataIndex
	go watch(d)
}
func watchConfig() {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		panic(err)
	}
	watcher.Add(confName)
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				println("config watcher not ok")
				continue
			}
			if event.Op&fsnotify.Write != fsnotify.Write {
				continue
			}
			go finishOld()
			go load()
		}
	}
}

func finishOld() {
	for _, d := range conf.List {
		if d.finish != nil {
			_, ok := <-d.finish
			if ok {
				d.finish <- true
				close(d.finish)
			}
		}
		if d.children == nil {
			continue
		}
		for _, c := range d.children {
			_, ok := <-c.finish
			if ok {
				c.finish <- true
				close(c.finish)
			}
		}
	}
}
func reload() {
	<-time.NewTimer(time.Second * 1).C
	load()
}
func watch(data *WatchData) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	data.watcher = watcher
	for _, w := range data.WatchList {
		for _, f := range w.Files {
			watcher.Add(f)
		}
		for _, f := range w.Folders {
			watchPath(f, watcher)
		}
	}

	mergeGroup(data)
	for {
		select {
		case <-data.finish:
			if data.terminted {
				println("finish yet", data.Name, data.index)
			}
			tryClose(data.finish)
			data.terminted = true
			println("finish", data.Name, data.index)
			err := watcher.Close()
			if err != nil {
				printError(err)
			}
			return
		case event, ok := <-watcher.Events:
			if !ok {
				continue
			}
			isCreate := event.Op&fsnotify.Create == fsnotify.Create
			isRename := event.Op&fsnotify.Rename == fsnotify.Rename
			isWrite := event.Op&fsnotify.Write == fsnotify.Write
			isDelete := event.Op&fsnotify.Remove == fsnotify.Remove
			if !isCreate && !isRename && !isWrite && !isDelete {
				continue
			}
			if (isCreate || isRename) && isDir(event.Name) {
				watchPath(event.Name, watcher)
			}
			go data.later()
		}
	}
}
func isDir(path string) bool {
	s, err := os.Stat(path)
	if err != nil {
		return false
	}
	return s.IsDir()
}
func watchPath(p string, w *fsnotify.Watcher) (err error) {
	w.Add(p)
	filepath.Walk(p, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() == false || p == path {
			return nil
		}
		return watchPath(path, w)
	})
	return
}
func tryClose(c chan bool) {
	_, o := <-c
	if o {
		close(c)
	}
}
func mergeGroup(data *WatchData) {
	println("merge group", data.Name, data.index)
	if data.terminted {
		println("live", data.Name, data.index)
		w := data.watcher
		if w != nil {
			e := w.Close()
			if e != nil {
				println("mergeGroup watcher close error", e)
			}
		}
		return
	}
	out := make(map[string]interface{})
	for _, w := range data.WatchList {
		for _, file := range w.Files {
			if err := mergeFile(out, file, data.KeyType); err != nil {
				printError(err)
				return
			}
		}
		for _, folder := range w.Folders {
			mergeFolder(data.Suffix, folder, data.KeyType, out)
		}
	}
	bf := bytes.NewBuffer([]byte{})
	jsonEncoder := json.NewEncoder(bf)
	jsonEncoder.SetIndent("", "  ")
	jsonEncoder.SetEscapeHTML(false)
	err := jsonEncoder.Encode(out)
	if err != nil {
		printError(err)
		return
	}
	//err = os.WriteFile(data.Output, []byte(bf.String()), 0644)
	err = os.WriteFile(data.Output, bf.Bytes(), 0644)
	if err != nil {
		printError(err)
	}
	c := 0
	if len(data.TsInterfaceOutput) > 0 {
		c++
	}
	if len(data.TsInterfaceName) > 0 {
		c++
	}
	if c == 0 {
		return
	}
	if c == 1 {
		println("ts interface output or name not set")
	}
	s := bf.String()
	exportInterface(s, data.TsInterfaceName, data.TsInterfaceOutput)
	checkKeysOutput(out, data)
	data.onChildChange()
	//r := regexp.MustCompile(`(: ".*")`)
	//res := r.ReplaceAllString(s, `:string`)
	//o := `export interface ` + data.TsInterfaceName + res
	//err = os.WriteFile(data.TsInterfaceOutput, []byte(o), 0644)
	//if err != nil {
	//	printError(err)
	//}
}

func exportInterface(contentStr, interfaceName, outputPath string) {
	r := regexp.MustCompile(`(: ".*")`)
	res := r.ReplaceAllString(contentStr, `:string`)
	o := `export interface ` + interfaceName + res
	err := os.WriteFile(outputPath, []byte(o), 0644)
	if err != nil {
		printError(err)
	}
}
func mergeFolder(suffix, folder, keyType string, out map[string]interface{}) (err error) {
	l, err := os.ReadDir(folder)
	if err != nil {
		printError(err)
		return
	}
	for _, f := range l {
		name := f.Name()
		file := path.Join(folder, name)
		if isDir(file) {
			parent := getSubMap(filepath.Base(file), keyType, out)
			mergeFolder(suffix, file, keyType, parent)
		}
		if strings.HasSuffix(name, suffix) == false {
			continue
		}
		//fileMap := getSubMap(name, keyType, out)
		if err = mergeFile(out, file, keyType); err != nil {
			printError(err)
			return
		}
	}
	return
}

func getSubMap(key, keyType string, parent map[string]interface{}) (m map[string]interface{}) {
	switch keyType {
	case KeyTypeJoinFolderFile:
		return getSubMapByKey(key, parent)
	case KeyTypeOriginal:
		return parent
	default:
		return parent
	}
}
func getSubMapByKey(key string, parent map[string]interface{}) (m map[string]interface{}) {
	value, ok := parent[key]
	var isMap bool
	if ok {
		m, isMap = mapify(value)
		if !isMap {
			//error
		}
	}
	if m == nil {
		m = map[string]interface{}{}
	}
	parent[key] = m
	return m
}
func mergeFile(out map[string]interface{}, file, keyType string) (err error) {
	bs, err := os.ReadFile(file)
	base := filepath.Base(file)
	name := base[0:strings.LastIndex(base, ".")]
	container := getSubMap(name, keyType, out)
	println(base, name)
	if len(bs) == 0 {
		err = errors.New("empty file")
	}
	if err != nil {
		return
	}
	m := map[string]interface{}{}
	err = json.Unmarshal(bs, &m)
	if err != nil {
		return
	}
	merge(container, m)
	return
}
func printError(err error) {
	println("err is", err)
}
func merge(dst, src map[string]interface{}) map[string]interface{} {
	for key, srcVal := range src {
		if dstVal, ok := dst[key]; ok {
			srcMap, srcMapOk := mapify(srcVal)
			dstMap, dstMapOk := mapify(dstVal)
			if srcMapOk && dstMapOk {
				srcVal = merge(dstMap, srcMap)
			}
		}
		dst[key] = srcVal
	}
	return dst
}

func mapify(i interface{}) (map[string]interface{}, bool) {
	value := reflect.ValueOf(i)
	if value.Kind() == reflect.Map {
		m := map[string]interface{}{}
		for _, k := range value.MapKeys() {
			m[k.String()] = value.MapIndex(k).Interface()
		}
		return m, true
	}
	return map[string]interface{}{}, false
}

func checkKeysOutput(m map[string]interface{}, data *WatchData) {
	if len(data.KeyOutput) == 0 {
		return
	}
	name := data.KeyValueName
	if len(name) == 0 {
		name = "i18nKeys"
	}
	km := map[string]string{}
	setMapKey(m, km, "")
	var i interface{} = m
	if data.KeyFlatten {
		i = km
	}
	b, e := json.Marshal(i)
	if e != nil {
		printError(e)
		return
	}
	o := `export const ` + name + " = " + string(b)
	e = os.WriteFile(data.KeyOutput, []byte(o), 0644)
	if e != nil {
		printError(e)
	}
}
func setMapKey(m map[string]interface{}, km map[string]string, parent string) {
	for k, v := range m {
		p := parent
		if len(p) > 0 {
			p = p + "."
		}
		p = p + k
		switch v.(type) {
		case string:
			m[k] = p
			km[p] = p
			break
		case map[string]interface{}:
			if mm, ok := v.(map[string]interface{}); ok {
				setMapKey(mm, km, p)
			}
			break
		default:
			panic("xx")
		}
	}
}
