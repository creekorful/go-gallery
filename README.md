# go-gallery

Generate a photography portfolio website for your photos.
A demo is available [here](https://photos.creekorful.org).

## Usage

```
$ ./go-gallery -photos-dir ~/Photos -output-dir /var/www/photos.example.org -config-file config.yaml
```

Where `-photos-dir` is the directory where the images are stored (*.jpg, *.jpeg), `-output-dir` is the directory where
the static website will be copied and `-config-file` is the path to the configuration file.

### The configuration file

```yaml
title: "go-gallery"
bg_color: "white"
border: "6px"
``` 

Once the website is generated you can upload it using lftp, rsync, etc.

The website may eventually be hosted on [Netlify](https://www.netlify.com/)
