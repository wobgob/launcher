//go:generate go-winres simply --icon favico.png
package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/schollz/progressbar/v3"
)

const (
	bucket = "wow335a"
	data   = "Data/patch-Z.mpq"
	status = "launcher.json"
)

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

func patch(client *minio.Client) bool {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	info, err := client.StatObject(ctx, bucket, data, minio.StatObjectOptions{})
	if err != nil {
		fmt.Println(err)
		return false
	}

	file, err := os.Open(info.Key)
	if err != nil {
		log.Println(err)
		return false
	}

	stat, err := os.Stat(info.Key)
	if err != nil {
		log.Println(err)
		return false
	}

	log.Printf("Checking %s\n", info.Key)
	bar := progressbar.DefaultBytes(stat.Size())
	hash := md5.New()
	io.Copy(io.MultiWriter(hash, bar), file)

	if hex.EncodeToString(hash.Sum(nil)) != info.ETag {
		log.Printf("Downloading %s\n", info.Key)
		object, err := client.GetObject(ctx, bucket, data, minio.GetObjectOptions{})
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
	endpoint := "cdn.wobgob.com"
	useSSL := true

	client, err := minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKeyID, secretAccessKey, ""),
		Secure: useSSL,
	})

	if err != nil {
		log.Println(err)
	}

	downloaded := download(client)
	if !downloaded {
		prompt()
		return
	}

	patched := patch(client)
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
