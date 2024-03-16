package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/fsnotify/fsnotify"
	"io/ioutil"
	"log"
	"os"
	"path"
	"reflect"
	"strings"
	"time"
)

type WatchList struct {
	Files   []string
	Folders []string
}
type WatchData struct {
	Name      string
	Output    string
	Suffix    string
	WatchList []*WatchList
	finish    chan bool
	index     int
	terminted bool
	timer     *time.Timer
	watcher   *fsnotify.Watcher
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
	//
	//ticker := time.NewTicker(time.Second * 3)
	//go func() {
	//	for _ = range ticker.C {
	//		go finishOld()
	//		go load()
	//	}
	//}()
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
		d.terminted = false
		d.finish = make(chan bool)
		watchDataIndex++
		println("carete", d.Name, watchDataIndex)
		d.index = watchDataIndex
		go watch(d)
	}
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
		_, ok := <-d.finish
		if ok {
			d.finish <- true
			close(d.finish)
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
			watcher.Add(f)
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
			//_, o := <-data.finish
			//if o {
			//	close(data.finish)
			//}
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
			if event.Op&fsnotify.Write != fsnotify.Write {
				continue
			}
			go data.later()
		}
	}
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
				println("eeee", e)
			}
		}
		return
	}
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
	bf := bytes.NewBuffer([]byte{})
	jsonEncoder := json.NewEncoder(bf)
	jsonEncoder.SetIndent("", "  ")
	jsonEncoder.SetEscapeHTML(false)
	err := jsonEncoder.Encode(out)
	if err != nil {
		printError(err)
		return
	}
	err = os.WriteFile(data.Output, []byte(bf.String()), 0644)
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
