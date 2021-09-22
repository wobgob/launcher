//go:generate go-winres simply --icon favico.png
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/google/go-github/v39/github"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/schollz/progressbar/v3"
	"golang.org/x/mod/semver"
)

const (
	version   = "v0.3.0"
	owner     = "wobgob"
	repo      = "launcher"
	exe       = "wow335a/launcher.exe"
	link      = "https://github.com/wobgob/launcher/releases/download/%s/wow335a.zip"
	bucket    = "wow335a"
	patchZ    = "Data/patch-Z.MPQ"
	realmlist = "Data/enUS/realmlist.wtf"
	status    = "launcher.json"
)

func readZip(file *zip.File) ([]byte, error) {
	f, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return ioutil.ReadAll(f)
}

func scream(err error, tagName string) {
	log.Println(err)
	log.Println("Manually install the update from " + fmt.Sprintf(link, tagName))
	prompt()
	os.Exit(1)
}

func update() (bool, error) {
	client := github.NewClient(nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	release, _, err := client.Repositories.GetLatestRelease(ctx, owner, repo)
	if err != nil {
		return false, err
	}

	exeName := os.Args[0]
	tagName := release.GetName()
	newer := semver.Compare(tagName, version)
	if newer != 1 {
		return false, nil
	}

	log.Printf("Updating %s\n", exeName)

	req, err := http.NewRequest("GET", fmt.Sprintf(link, tagName), nil)
	if err != nil {
		return false, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	buffer := new(bytes.Buffer)
	bar := progressbar.DefaultBytes(resp.ContentLength)
	io.Copy(io.MultiWriter(buffer, bar), resp.Body)

	body, err := ioutil.ReadAll(buffer)
	if err != nil {
		return false, err
	}

	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return false, err
	}

	var exeBytes []byte
	found := false
	for _, file := range reader.File {
		if file.Name == exe {
			found = true
			exeBytes, err = readZip(file)
			if err != nil {
				log.Println(err)
				return false, err
			}
		}
	}

	if !found {
		return false, errors.New(fmt.Sprintf("Unable to get %s.", exe))
	}

	err = os.Rename(exeName, exeName+"~")
	if err != nil {
		log.Println(err)
		return false, err
	}

	out, err := os.Create(exeName)
	if err != nil {
		scream(err, tagName)
	}

	_, err = io.Copy(out, bytes.NewReader(exeBytes))
	if err != nil {
		scream(err, tagName)
	}

	err = out.Close()
	if err != nil {
		scream(err, tagName)
	}

	cmd := exec.Command(exeName)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err = cmd.Start()
	if err != nil {
		scream(err, tagName)
	}

	return true, nil
}

func write(file **os.File, objects *map[string]bool, result *bool) {
	enc := json.NewEncoder(*file)
	enc.SetIndent("", "\t")
	err := enc.Encode(*objects)
	if err != nil {
		log.Println(err)
		*result = false
	}
}

func handleConsoleCtrl(c chan<- os.Signal) error {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleCtrlHandler := kernel32.NewProc("SetConsoleCtrlHandler")

	n, _, err := setConsoleCtrlHandler.Call(
		syscall.NewCallback(func(controlType uint) uint {
			if controlType >= 2 {
				c <- syscall.Signal(0x1f + controlType)

				select {} // blocks forever
			}

			return 0
		}),
		1,
	)

	if n == 0 {
		return err
	}

	return nil
}

func download(client *minio.Client) (result bool) {
	var file *os.File
	objects := make(map[string]bool)
	_, err := os.Stat(status)
	if err == nil {
		file, err = os.Open(status)
		if err != nil {
			log.Println(err)
			return false
		}

		dec := json.NewDecoder(file)
		err = dec.Decode(&objects)
		if err != nil {
			log.Println(err)
			return false
		}

		os.Remove(status)
	} else if err != nil && os.IsExist(err) {
		log.Println(err)
		return false
	}

	file, err = os.Create(status)
	if err != nil {
		log.Println(err)
		return false
	}

	sig := make(chan os.Signal, 1)
	if err := handleConsoleCtrl(sig); err != nil {
		log.Println(err)
		return false
	}
	signal.Notify(sig, os.Interrupt)
	go func() {
		for range sig {
			write(&file, &objects, &result)
			os.Exit(1)
		}
	}()

	defer write(&file, &objects, &result)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	list := client.ListObjects(ctx, bucket, minio.ListObjectsOptions{
		Recursive: true,
	})

	for info := range list {
		if info.Err != nil {
			log.Println(info.Err)
			return false
		}

		_, err := os.Stat(info.Key)
		complete, ok := objects[info.Key]
		switch {
		case err == nil && (!ok || (ok && complete)):
			break
		case err == nil && ok && !complete:
			err = os.Remove(info.Key)
			if err != nil {
				log.Println(err)
				return false
			}
			fallthrough
		case os.IsNotExist(err):
			dir := filepath.Dir(info.Key)
			_, err := os.Stat(dir)
			if os.IsNotExist(err) {
				err = os.MkdirAll(dir, 0755)
				log.Printf("Created %s\n", dir)
			} else if err != nil {
				log.Println(err)
				return false
			}

			object, err := client.GetObject(ctx, bucket, info.Key, minio.GetObjectOptions{})
			if err != nil {
				log.Println(err)
				return false
			}

			f, err := os.Create(info.Key)
			if err != nil {
				log.Println(err)
				return false
			}

			log.Printf("Downloading %s\n", info.Key)
			bar := progressbar.DefaultBytes(info.Size)
			objects[info.Key] = false
			if _, err = io.Copy(io.MultiWriter(f, bar), object); err != nil {
				log.Println(err)
				return false
			}
			objects[info.Key] = true
		default:
			log.Println(err)
			return false
		}
	}

	return true
}

func patch(client *minio.Client, filename string) bool {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	info, err := client.StatObject(ctx, bucket, filename, minio.StatObjectOptions{})
	if err != nil {
		log.Println(err)
		return false
	}

	stat, err := os.Stat(info.Key)
	if err != nil {
		log.Println(err)
		return false
	}

	file, err := os.Open(info.Key)
	if err != nil {
		log.Println(err)
		return false
	}

	log.Printf("Checking %s\n", info.Key)
	bar := progressbar.DefaultBytes(stat.Size())
	hash := md5.New()
	io.Copy(io.MultiWriter(hash, bar), file)

	err = file.Close()
	if err != nil {
		log.Println(err)
		return false
	}

	if hex.EncodeToString(hash.Sum(nil)) != info.ETag {
		err = os.Remove(info.Key)
		if err != nil {
			log.Println(err)
			return false
		}

		file, err = os.Create(info.Key)
		if err != nil {
			log.Println(err)
			return false
		}

		log.Printf("Downloading %s\n", info.Key)
		object, err := client.GetObject(ctx, bucket, filename, minio.GetObjectOptions{})
		if err != nil {
			log.Println(err)
			return false
		}

		bar := progressbar.DefaultBytes(stat.Size())
		if _, err = io.Copy(io.MultiWriter(file, bar), object); err != nil {
			log.Println(err)
			return false
		}
	}

	return true
}

func prompt() {
	fmt.Print("Press 'Enter' to continue...")
	fmt.Scanln()
}

func main() {
	exeName := os.Args[0]
	err := os.RemoveAll(exeName + "~")
	if err != nil {
		log.Println(err)
		prompt()
		return
	}

	endpoint := "cdn.wobgob.com"
	useSSL := true

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
	})

	if err != nil {
		log.Println(err)
		prompt()
		return
	}

	updated, err := update()
	if err != nil {
		log.Println(err)
		prompt()
		return
	}

	if updated {
		return
	}

	downloaded := download(client)
	if !downloaded {
		prompt()
		return
	}

	patched := patch(client, patchZ)
	if !patched {
		prompt()
		return
	}

	patched = patch(client, realmlist)
	if !patched {
		prompt()
		return
	}

	log.Println("Launching Wow.exe")
	cmd := exec.Command("./Wow.exe")

	err = cmd.Start()
	if err != nil {
		log.Println(err)
		prompt()
		return
	}

	err = cmd.Wait()
	if err != nil {
		log.Println(err)
		prompt()
		return
	}
}
