package main

import (
	"bytes"
	"embed"
	_ "embed"
	"flag"
	"github.com/nfnt/resize"
	"github.com/rwcarlsen/goexif/exif"
	"gopkg.in/yaml.v2"
	"html/template"
	"image/jpeg"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"
)

var (
	// the program version, exported using LDFLAGS
	version = "dev"

	photosDirFlag  = flag.String("photos-dir", "photos", "")
	outputDirFlag  = flag.String("output-dir", "dist", "")
	configFileFlag = flag.String("config-file", "config.yaml", "")

	//go:embed res/*
	resDirectory embed.FS
)

// the program configuration file
type config struct {
	Title   string `yaml:"title"`
	BgColor string `yaml:"bg_color"`
	Border  string `yaml:"border"`
}

type context struct {
	Config config
	Photos []map[string]interface{}
}

func main() {
	flag.Parse()

	log.Printf("running go-gallery %s", version)

	config, err := readConfig()
	if err != nil {
		log.Fatalf("error while reading config: %s", err)
	}

	// Create dist folder
	if err := os.Mkdir(*outputDirFlag, 0750); err != nil && !os.IsExist(err) {
		log.Fatalf("error while creating %s/ folder: %s", *outputDirFlag, err)
	}

	photos, err := processImages(*photosDirFlag, *outputDirFlag)
	if err != nil {
		log.Fatalf("error while processing images: %s", err)
	}

	ctx := context{Config: config, Photos: photos}

	// Generate the index.html
	if err := generateIndex(ctx, *outputDirFlag); err != nil {
		log.Fatalf("error while generating index.html: %s", err)
	}

	// Generate the index.css
	if err := generateStylesheet(ctx, *outputDirFlag); err != nil {
		log.Fatalf("error while generating index.css: %s", err)
	}

	// Copy the third party files
	files, err := resDirectory.ReadDir(filepath.Join("res", "vendor"))
	if err != nil {
		log.Fatalf("error while processing res/vendor: %s", err)
	}

	for _, file := range files {
		fp := filepath.Join("res", "vendor", file.Name())

		content, err := resDirectory.ReadFile(fp)
		if err != nil {
			log.Fatalf("error while copying 3rd party file %s: %s", fp, err)
		}
		if err := ioutil.WriteFile(filepath.Join(*outputDirFlag, file.Name()), content, 0640); err != nil {
			log.Fatalf("error while copying 3rd party file %s: %s", fp, err)
		}
	}

	log.Printf("successfully generated! (%d photos)", len(photos))
}

func readConfig() (config, error) {
	f, err := os.Open(*configFileFlag)
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

func generateIndex(ctx context, distDirectory string) error {
	t, err := template.New("index.html.tmpl").ParseFS(resDirectory, "res/index.html.tmpl")
	if err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(distDirectory, "index.html"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := t.ExecuteTemplate(f, "index.html.tmpl", ctx); err != nil {
		return err
	}

	return nil
}

func generateStylesheet(ctx context, distDirectory string) error {
	t, err := template.New("index.css.tmpl").ParseFS(resDirectory, "res/index.css.tmpl")
	if err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(distDirectory, "index.css"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := t.ExecuteTemplate(f, "index.css.tmpl", ctx); err != nil {
		return err
	}

	return nil
}

func processImages(photosDir, outputDir string) ([]map[string]interface{}, error) {
	if err := os.MkdirAll(filepath.Join(outputDir, "photos", "thumbnails"), 0750); err != nil {
		return nil, err
	}

	var photos []map[string]interface{}

	if err := filepath.Walk(photosDir, func(path string, info fs.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		// Read the image
		imgBytes, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Determinate if the image is not already processed (i.e copied to dist/ and thumbnail generated)
		_, err = os.Stat(filepath.Join(outputDir, "photos", info.Name()))
		if os.IsNotExist(err) {
			log.Printf("processing: %s", path)

			// Generate thumbnail
			img, err := jpeg.Decode(bytes.NewReader(imgBytes))
			if err != nil {
				return err
			}
			thumbFile, err := os.Create(filepath.Join(outputDir, "photos", "thumbnails", info.Name()))
			if err != nil {
				return err
			}
			img = resize.Resize(640, 0, img, resize.Lanczos3)
			if err := jpeg.Encode(thumbFile, img, nil); err != nil {
				return err
			}

			// Copy the image
			if err := ioutil.WriteFile(filepath.Join(outputDir, "photos", info.Name()), imgBytes, 0640); err != nil {
				return err
			}
		} else {
			log.Printf("skipping existing file: %s", info.Name())
		}

		photo := map[string]interface{}{
			"Title":         info.Name(),
			"ImgPath":       filepath.Join("photos", info.Name()),
			"ThumbnailPath": filepath.Join("photos", "thumbnails", info.Name()),
		}

		// Try to parse image EXIF data to get the shooting date
		if x, err := exif.Decode(bytes.NewReader(imgBytes)); err == nil {
			if tag, err := x.Get(exif.DateTimeOriginal); err == nil {
				if dateTimeStr, err := tag.StringVal(); err == nil {
					if dateTime, err := time.Parse("2006:01:02 15:04:05", dateTimeStr); err == nil {
						photo["ShootingDate"] = dateTime
					}
				}
			}
		}

		photos = append(photos, photo)

		return nil
	}); err != nil {
		return nil, err
	}

	// sort the photos by shooting date if available
	// otherwise fallback to filename
	sort.SliceStable(photos, func(left, right int) bool {
		leftDateTime := time.Time{}
		if val, exists := photos[left]["ShootingDate"]; exists {
			leftDateTime = val.(time.Time)
		}

		rightDateTime := time.Time{}
		if val, exists := photos[right]["ShootingDate"]; exists {
			rightDateTime = val.(time.Time)
		}

		if !leftDateTime.IsZero() && !rightDateTime.IsZero() {
			return leftDateTime.After(rightDateTime)
		}

		// otherwise, fallback to filename comparison
		return photos[left]["Title"].(string) > photos[right]["Title"].(string)
	})

	return photos, nil
}
