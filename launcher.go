package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/schollz/progressbar/v3"
)

const (
	wow335a = "wow335a"
	patches = "Data/patch-4.mpq"
)

func download(client *minio.Client) bool {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	objects := client.ListObjects(ctx, wow335a, minio.ListObjectsOptions{
		Recursive: true,
	})

	for info := range objects {
		if info.Err != nil {
			log.Println(info.Err)
			return false
		}

		_, err := os.Stat(info.Key)
		if os.IsNotExist(err) {

			dir := filepath.Dir(info.Key)
			_, err := os.Stat(dir)
			if os.IsNotExist(err) {
				err = os.MkdirAll(dir, 0755)
				log.Printf("Created directory %s\n", dir)
			} else if err != nil {
				log.Println(err)
				return false
			}

			object, err := client.GetObject(ctx, wow335a, info.Key, minio.GetObjectOptions{})
			if err != nil {
				log.Println(err)
				return false
			}

			file, err := os.Create(info.Key)
			if err != nil {
				log.Println(err)
				return false
			}

			log.Printf("Downloading %s\n", info.Key)
			bar := progressbar.DefaultBytes(info.Size)
			if _, err = io.Copy(io.MultiWriter(file, bar), object); err != nil {
				log.Println(err)
				return false
			}
		} else if err != nil {
			log.Println(err)
			return false
		}
	}

	return true
}

func patch(client *minio.Client) bool {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	info, err := client.StatObject(context.Background(), wow335a, patches, minio.StatObjectOptions{})
	if err != nil {
		fmt.Println(err)
		return false
	}

	object, err := client.GetObject(ctx, wow335a, patches, minio.GetObjectOptions{})
	if err != nil {
		log.Println(err)
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
	}

	log.Printf("Checking %s\n", info.Key)
	bar := progressbar.DefaultBytes(stat.Size())
	hash := md5.New()
	io.Copy(io.MultiWriter(hash, bar), file)

	if hex.EncodeToString(hash.Sum(nil)) != info.ETag {
		log.Printf("Downloading %s\n", info.Key)
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
	// Unprivileged user with read-only access to `wow335a`. Better than
	// anonymous access.
	accessKeyID := "AEfyf0EpQdQ602Ri"
	secretAccessKey := "Lhn5MR8IGWkiS1lvWNebb0DmrwIx4uF3kXQl4odfkOwkgr7ci0"
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
