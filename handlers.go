package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func isHashed(dir string) bool {
	return dir == "data"
}

func createDirectories(path string) {
	log.Println("Creating repository directories")

	if err := os.MkdirAll(path, 0700); err != nil {
		log.Fatal(err)
	}

	dirs := []string{
		"data",
		"index",
		"keys",
		"locks",
		"snapshots",
		"tmp",
	}

	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(path, d), 0700); err != nil {
			log.Fatal(err)
		}
	}

	for i := 0; i < 256; i++ {
		if err := os.MkdirAll(filepath.Join(path, "data", fmt.Sprintf("%02x", i)), 0700); err != nil {
			log.Fatal(err)
		}
	}
}

// AuthHandler wraps h with a http.HandlerFunc that performs basic authentication against the user/passwords pairs
// stored in f and returns the http.HandlerFunc.
func AuthHandler(f *HtpasswdFile, h http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if username, password, ok := r.BasicAuth(); !ok || !f.Validate(username, password) {
			http.Error(w, "401 unauthorized", 401)
			return
		}

		h.ServeHTTP(w, r)
	}
}

// CheckConfig checks whether a configuration exists.
func CheckConfig(w http.ResponseWriter, r *http.Request) {
	if *debug {
		log.Println("CheckConfig()")
	}
	config := filepath.Join(*path, "config")
	st, err := os.Stat(config)
	if err != nil {
		http.Error(w, "404 not found", 404)
		return
	}

	w.Header().Add("Content-Length", fmt.Sprint(st.Size()))
}

// GetConfig allows for a config to be retrieved.
func GetConfig(w http.ResponseWriter, r *http.Request) {
	if *debug {
		log.Println("GetConfig()")
	}
	config := filepath.Join(*path, "config")
	bytes, err := ioutil.ReadFile(config)
	if err != nil {
		http.Error(w, "404 not found", 404)
		return
	}

	w.Write(bytes)
}

// SaveConfig allows for a config to be saved.
func SaveConfig(w http.ResponseWriter, r *http.Request) {
	if *debug {
		log.Println("SaveConfig()")
	}
	config := filepath.Join(*path, "config")
	bytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "400 bad request", 400)
		return
	}
	if err := ioutil.WriteFile(config, bytes, 0600); err != nil {
		http.Error(w, "500 internal server error", 500)
		return
	}

	w.Write([]byte("200 ok"))
}

// ListBlobs lists all blobs of a given type in an arbitrary order.
func ListBlobs(w http.ResponseWriter, r *http.Request) {
	if *debug {
		log.Println("ListBlobs()")
	}
	vars := strings.Split(r.RequestURI, "/")
	dir := vars[1]
	path := filepath.Join(*path, dir)

	items, err := ioutil.ReadDir(path)
	if err != nil {
		http.Error(w, "404 not found", 404)
		return
	}

	var names []string
	for _, i := range items {
		if isHashed(dir) {
			subpath := filepath.Join(path, i.Name())
			subitems, err := ioutil.ReadDir(subpath)
			if err != nil {
				http.Error(w, "404 not found", 404)
				return
			}
			for _, f := range subitems {
				names = append(names, f.Name())
			}
		} else {
			names = append(names, i.Name())
		}
	}

	data, err := json.Marshal(names)
	if err != nil {
		http.Error(w, "500 internal server error", 500)
		return
	}

	w.Write(data)
}

// CheckBlob tests whether a blob exists and returns 200, if it does, or 404 otherwise.
func CheckBlob(w http.ResponseWriter, r *http.Request) {
	if *debug {
		log.Println("CheckBlob()")
	}
	vars := strings.Split(r.RequestURI, "/")
	dir := vars[1]
	name := vars[2]

	if isHashed(dir) {
		name = filepath.Join(name[:2], name)
	}
	path := filepath.Join(*path, dir, name)

	st, err := os.Stat(path)
	if err != nil {
		http.Error(w, "404 not found", 404)
		return
	}

	w.Header().Add("Content-Length", fmt.Sprint(st.Size()))
}

// GetBlob retrieves a blob from the repository.
func GetBlob(w http.ResponseWriter, r *http.Request) {
	if *debug {
		log.Println("GetBlob()")
	}
	vars := strings.Split(r.RequestURI, "/")
	dir := vars[1]
	name := vars[2]

	if isHashed(dir) {
		name = filepath.Join(name[:2], name)
	}
	path := filepath.Join(*path, dir, name)

	file, err := os.Open(path)
	if err != nil {
		http.Error(w, "404 not found", 404)
		return
	}

	http.ServeContent(w, r, "", time.Unix(0, 0), file)
	file.Close()
}

// SaveBlob saves a blob to the repository.
func SaveBlob(w http.ResponseWriter, r *http.Request) {
	if *debug {
		log.Println("SaveBlob()")
	}
	vars := strings.Split(r.RequestURI, "/")
	dir := vars[1]
	name := vars[2]

	if dir == "keys" {
		if _, err := os.Stat("keys"); err != nil && os.IsNotExist(err) {
			createDirectories(*path)
		}
	}

	tmp := filepath.Join(*path, "tmp", name)

	tf, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		http.Error(w, "500 internal server error", 500)
		return
	}

	if _, err := io.Copy(tf, r.Body); err != nil {
		tf.Close()
		os.Remove(tmp)
		http.Error(w, "400 bad request", 400)
		return
	}
	if err := tf.Sync(); err != nil {
		tf.Close()
		os.Remove(tmp)
		http.Error(w, "500 internal server error", 500)
		return
	}
	if err := tf.Close(); err != nil {
		os.Remove(tmp)
		http.Error(w, "500 internal server error", 500)
		return
	}

	if isHashed(dir) {
		name = filepath.Join(name[:2], name)
	}
	path := filepath.Join(*path, dir, name)

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		os.Remove(path)
		http.Error(w, "500 internal server error", 500)
		return
	}

	w.Write([]byte("200 ok"))
}

// DeleteBlob deletes a blob from the repository.
func DeleteBlob(w http.ResponseWriter, r *http.Request) {
	if *debug {
		log.Println("DeleteBlob()")
	}
	vars := strings.Split(r.RequestURI, "/")
	dir := vars[1]
	name := vars[2]

	if isHashed(dir) {
		name = filepath.Join(name[:2], name)
	}
	path := filepath.Join(*path, dir, name)

	if err := os.Remove(path); err != nil {
		http.Error(w, "500 internal server error", 500)
		return
	}

	w.Write([]byte("200 ok"))
}
