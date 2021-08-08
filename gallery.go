package main

import (
	"bytes"
	"embed"
	_ "embed"
	"flag"
	"github.com/nfnt/resize"
	"gopkg.in/yaml.v2"
	"html/template"
	"image/jpeg"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sort"
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
	Title string `yaml:"title"`
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

	// Generate the index.html
	if err := generateIndex(context{Config: config, Photos: photos}, *outputDirFlag); err != nil {
		log.Fatalf("error while generating index.html: %s", err)
	}

	// Copy the index.css
	if err := copyCSSStyle(*outputDirFlag); err != nil {
		log.Fatalf("error while copying index.css: %s", err)
	}

	log.Printf("successfully generated!")
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
	t, err := template.New("index.gohtml").ParseFS(resDirectory, "res/index.gohtml")
	if err != nil {
		return err
	}

	f, err := os.OpenFile(filepath.Join(distDirectory, "index.html"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0640)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := t.ExecuteTemplate(f, "index.gohtml", ctx); err != nil {
		return err
	}

	return nil
}

func copyCSSStyle(distDirectory string) error {
	style, err := resDirectory.ReadFile("res/index.css")
	if err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(distDirectory, "index.css"), style, 0640); err != nil {
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

		log.Printf("processing %s", path)

		_, err = os.Stat(filepath.Join(outputDir, "photos", info.Name()))
		if os.IsNotExist(err) {
			imgBytes, err := os.ReadFile(path)
			if err != nil {
				return err
			}

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

		photos = append(photos, map[string]interface{}{
			"Title":         info.Name(),
			"ImgPath":       filepath.Join("photos", info.Name()),
			"ThumbnailPath": filepath.Join("photos", "thumbnails", info.Name()),
		})

		return nil
	}); err != nil {
		return nil, err
	}

	// sort the photos by filename
	// TODO: should sort by photo taken date if information is available
	sort.SliceStable(photos, func(i, j int) bool {
		return photos[i]["Title"].(string) > photos[j]["Title"].(string)
	})

	return photos, nil
}
