package main

import (
	"bytes"
	"crypto/md5"
	"embed"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"flag"
	"github.com/nfnt/resize"
	"github.com/rwcarlsen/goexif/exif"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v2"
	"html/template"
	"image/jpeg"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	// the program version, exported using LDFLAGS
	version = "dev"

	photosDirFlag  = flag.String("photos-dir", "", "")
	distDirFlag    = flag.String("output-dir", "dist", "")
	configFileFlag = flag.String("config-file", "config.yaml", "")

	//go:embed res/*
	resDirectory embed.FS

	filePerm = os.FileMode(0640)
	dirPerm  = os.FileMode(0750)
)

// the program configuration file
type config struct {
	Title            string `yaml:"title"`
	BgColor          string `yaml:"bg_color"`
	FontColor        string `yaml:"font_color"`
	Border           string `yaml:"border"`
	ThumbnailMaxSize uint   `yaml:"thumbnail_max_size"`
	ShowSeparator    bool   `yaml:"show_separator"`
}

type context struct {
	Config config
	Photos []photo
}

type photo struct {
	Title         string    `json:"title"`
	PhotoPath     string    `json:"photo_path"`
	ThumbnailPath string    `json:"thumbnail_path"`
	ShootingDate  time.Time `json:"shooting_date,omitempty"`
	PhotoChecksum string    `json:"photo_checksum"`
}

type index struct {
	Photos []photo `json:"photos"`
}

func main() {
	flag.Parse()

	log.Printf("running go-gallery %s", version)

	// Validate parameters
	if *photosDirFlag == "" {
		log.Fatalf("missing required parameter --photos-dir")
	}

	// Make sure photosDir exists
	_, err := os.Stat(*photosDirFlag)
	if os.IsNotExist(err) {
		log.Fatalf("directory %s does not exist", *photosDirFlag)
	}

	config, err := readConfig(*configFileFlag)
	if err != nil {
		log.Fatalf("error while reading config: %s", err)
	}

	// Create dist folder
	if err := os.MkdirAll(*distDirFlag, dirPerm); err != nil {
		log.Fatalf("error while creating %s/ folder: %s", *distDirFlag, err)
	}

	// Read the previous index
	previousIndex := index{}
	b, err := ioutil.ReadFile(filepath.Join(*distDirFlag, "index.json"))
	if err == nil {
		if err := json.Unmarshal(b, &previousIndex); err != nil {
			log.Fatalf("error while reaading index.json: %s", err)
		}
	} else if !os.IsNotExist(err) {
		log.Fatalf("error while reaading index.json: %s", err)
	}

	photos, err := processPhotos(*photosDirFlag, *distDirFlag, config.ThumbnailMaxSize, previousIndex)
	if err != nil {
		log.Fatalf("error while processing photos: %s", err)
	}

	ctx := context{Config: config, Photos: photos}

	// Generate the index.json
	indexBytes, err := json.Marshal(index{Photos: photos})
	if err != nil {
		log.Fatalf("error while generating index.json: %s", err)
	}
	if err := ioutil.WriteFile(filepath.Join(*distDirFlag, "index.json"), indexBytes, filePerm); err != nil {
		log.Fatalf("error while generating index.json: %s", err)
	}

	// Generate the index.html
	if err := executeTemplate(ctx, *distDirFlag, "index.html.tmpl"); err != nil {
		log.Fatalf("error while generating index.html: %s", err)
	}

	// Generate the index.css
	if err := executeTemplate(ctx, *distDirFlag, "index.css.tmpl"); err != nil {
		log.Fatalf("error while generating index.css: %s", err)
	}

	// Copy the third party files
	files, err := resDirectory.ReadDir(filepath.Join("res", "vendor"))
	if err != nil {
		log.Fatalf("error while processing res/vendor: %s", err)
	}

	for _, file := range files {
		srcPath := filepath.Join("vendor", file.Name())
		destPath := filepath.Join(*distDirFlag, file.Name())

		if err := copyResFile(srcPath, destPath); err != nil {
			log.Fatalf("error while copying 3rd party file %s: %s", srcPath, err)
		}
	}

	// Copy the favicon
	if err := copyResFile(filepath.Join("favicon.png"), filepath.Join(*distDirFlag, "favicon.png")); err != nil {
		log.Fatalf("error while copying favicon: %s", err)
	}

	log.Printf("successfully generated! (%d photos)", len(photos))
}

func readConfig(path string) (config, error) {
	f, err := os.Open(path)
	if err != nil {
		return config{}, err
	}
	defer f.Close()

	var c config
	if err := yaml.NewDecoder(f).Decode(&c); err != nil {
		return config{}, err
	}

	return c, nil
}

func executeTemplate(ctx context, distDirectory, templateName string) error {
	t, err := template.
		New(templateName).
		Funcs(map[string]interface{}{
			"samePeriod": func(photos []photo, idx int) bool {
				// First photo
				if idx-1 < 0 {
					return false
				}

				left := photos[idx-1]
				right := photos[idx]

				leftShootingDate := left.ShootingDate
				rightShootingDate := right.ShootingDate

				return leftShootingDate.Year() == rightShootingDate.Year() &&
					leftShootingDate.Month() == rightShootingDate.Month()
			},
		}).
		ParseFS(resDirectory, filepath.Join("res", templateName))
	if err != nil {
		return err
	}

	dstPath := filepath.Join(distDirectory, strings.TrimSuffix(templateName, ".tmpl"))
	f, err := os.OpenFile(dstPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, filePerm)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := t.ExecuteTemplate(f, templateName, ctx); err != nil {
		return err
	}

	return nil
}

func processPhotos(photosDir, distDirectory string, thumbnailMaxSize uint, previousIndex index) ([]photo, error) {
	if err := os.MkdirAll(filepath.Join(distDirectory, "photos", "thumbnails"), dirPerm); err != nil {
		return nil, err
	}

	var photos []photo

	workers := errgroup.Group{}
	photosMutex := sync.Mutex{}

	if err := filepath.Walk(photosDir, func(path string, info fs.FileInfo, err error) error {
		workers.Go(func() error {
			if !isJpegFile(info) {
				return nil
			}

			// Read the photo
			photoBytes, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			photo := photo{}

			// Determinate if the photo is not already processed
			if !isPhotoProcessed(photoBytes, info.Name(), previousIndex) {
				log.Printf("processing %s", info.Name())

				photo, err = processPhoto(photoBytes, thumbnailMaxSize, info.Name(), distDirectory)
				if err != nil {
					log.Fatalf("error while processing photo %s: %s", info.Name(), err)
				}
			} else {
				// use already processed photo
				log.Printf("skipping unchanged photo %s", info.Name())

				for _, previousPhoto := range previousIndex.Photos {
					if previousPhoto.Title == info.Name() {
						photo = previousPhoto
						break
					}
				}
			}

			photosMutex.Lock()
			photos = append(photos, photo)
			photosMutex.Unlock()

			return nil
		})

		return workers.Wait()
	}); err != nil {
		return nil, err
	}

	// sort the photos by shooting date if available
	// otherwise fallback to filename
	sort.SliceStable(photos, func(left, right int) bool {
		leftDateTime := time.Time{}
		if val := photos[left].ShootingDate; !val.IsZero() {
			leftDateTime = val
		}

		rightDateTime := time.Time{}
		if val := photos[right].ShootingDate; !val.IsZero() {
			rightDateTime = val
		}

		if !leftDateTime.IsZero() && !rightDateTime.IsZero() {
			return leftDateTime.After(rightDateTime)
		}

		// otherwise, fallback to filename comparison
		return photos[left].Title > photos[right].Title
	})

	return photos, nil
}

func isPhotoProcessed(photoBytes []byte, photoTitle string, previousIndex index) bool {
	photoIdx := -1

	for i, current := range previousIndex.Photos {
		if current.Title == photoTitle {
			photoIdx = i
			break
		}
	}

	if photoIdx == -1 {
		return false
	}

	hash := md5.Sum(photoBytes)

	return previousIndex.Photos[photoIdx].PhotoChecksum == hex.EncodeToString(hash[:])
}

func processPhoto(photoBytes []byte, thumbnailMaxSize uint, photoTitle, distDirectory string) (photo, error) {
	photoDstPath := filepath.Join(distDirectory, "photos", photoTitle)
	thumbnailDstPath := filepath.Join(distDirectory, "photos", "thumbnails", photoTitle)

	// Generate thumbnail
	photoImg, err := jpeg.Decode(bytes.NewReader(photoBytes))
	if err != nil {
		return photo{}, err
	}
	thumbFile, err := os.Create(thumbnailDstPath)
	if err != nil {
		return photo{}, err
	}

	photoImg = resize.Thumbnail(thumbnailMaxSize, thumbnailMaxSize, photoImg, resize.MitchellNetravali)
	if err := jpeg.Encode(thumbFile, photoImg, nil); err != nil {
		return photo{}, err
	}

	// Copy the photo
	if err := ioutil.WriteFile(photoDstPath, photoBytes, filePerm); err != nil {
		return photo{}, err
	}

	photo := photo{
		Title:         photoTitle,
		PhotoPath:     filepath.Join("photos", photoTitle),
		ThumbnailPath: filepath.Join("photos", "thumbnails", photoTitle),
	}

	// Generate the MD5 of the photos to check for changes on later execution
	hash := md5.Sum(photoBytes)
	photo.PhotoChecksum = hex.EncodeToString(hash[:])

	// Try to parse photo EXIF data to get the shooting date
	if x, err := exif.Decode(bytes.NewReader(photoBytes)); err == nil {
		if tag, err := x.Get(exif.DateTimeOriginal); err == nil {
			if dateTimeStr, err := tag.StringVal(); err == nil {
				if dateTime, err := time.Parse("2006:01:02 15:04:05", dateTimeStr); err == nil {
					photo.ShootingDate = dateTime
				}
			}
		}
	}

	return photo, nil
}

func isJpegFile(file fs.FileInfo) bool {
	return !file.IsDir() && (strings.HasSuffix(file.Name(), ".jpg") || strings.HasSuffix(file.Name(), ".jpeg"))
}

func copyResFile(srcPath, dstPath string) error {
	content, err := resDirectory.ReadFile(filepath.Join("res", srcPath))
	if err != nil {
		return err
	}

	return ioutil.WriteFile(dstPath, content, filePerm)
}
