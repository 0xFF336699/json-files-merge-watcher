package main

import (
	"encoding/json"
	"errors"
	"github.com/fsnotify/fsnotify"
	"io/ioutil"
	"log"
	"os"
	"path"
	"reflect"
	"strings"
)

type WatchList struct {
	Files   []string
	Folders []string
}
type WatchData struct {
	Output    string
	Suffix    string
	WatchList []*WatchList
}

type Conf struct {
	List []*WatchData
}

func main() {
	run()
}

func run() {
	configBytes, err := ioutil.ReadFile("config.json")
	if err != nil {
		log.Fatal(err)
	}

	var conf Conf
	err = json.Unmarshal(configBytes, &conf)
	if err != nil {
		log.Fatal(err)
	}
	for _, d := range conf.List {
		go watch(d)
	}
	select {}
}
func watch(data *WatchData) {

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	for _, w := range data.WatchList {
		for _, f := range w.Files {
			watcher.Add(f)
		}
		for _, f := range w.Folders {
			watcher.Add(f)
		}
	}
	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write != fsnotify.Write {
				continue
			}
			mergeGroup(data)
		}
	}
}

func mergeGroup(data *WatchData) {
	out := make(map[string]interface{})
	for _, w := range data.WatchList {
		for _, file := range w.Files {
			if err := mergeFile(out, file); err != nil {
				printError(err)
				return
			}
		}
		for _, folder := range w.Folders {
			l, err := os.ReadDir(folder)
			if err != nil {
				printError(err)
				return
			}
			for _, f := range l {
				name := f.Name()
				if strings.HasSuffix(name, data.Suffix) == false {
					continue
				}
				file := path.Join(folder, name)
				if err = mergeFile(out, file); err != nil {
					printError(err)
					return
				}
			}
		}
	}
	bs, err := json.Marshal(out)
	if err != nil {
		printError(err)
		return
	}
	err = os.WriteFile(data.Output, bs, 0644)
	if err != nil {
		printError(err)
	}
}

func mergeFile(out map[string]interface{}, file string) (err error) {
	bs, err := os.ReadFile(file)
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
	merge(out, m)
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
