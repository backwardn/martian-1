//
// Copyright (c) 2015 10X Genomics, Inc. All rights reserved.
//
// Houston AWS S3 downloader.
//

package main

import (
	"fmt"
	"martian/core"
	"martian/manager"
	"net/http"
	"os"
	"os/exec"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/dustin/go-humanize"
)

//const MAXDOWNLOAD = 5 * 1000 * 1000 * 1000 // 5GB
const DOWNLOAD_MAXIMUM = 1 * 1000 * 1000 // 1MB
const DOWNLOAD_INTERVAL = 2              // minutes

type Download struct {
	size     uint64
	year     string
	month    string
	day      string
	user     string
	domain   string
	uid      string
	fname    string
	ftype    string
	fdir     string
	permPath string
}

func NewDownload(stPath string, size uint64, year string, month string, day string, user string, domain string, uid string, fname string) *Download {
	self := &Download{}
	self.size = size
	self.year = year
	self.month = month
	self.day = day
	self.user = user
	self.domain = domain
	self.uid = uid
	self.fname = fname
	self.ftype = path.Ext(self.fname)
	self.fdir = fmt.Sprintf("%s%s", self.uid, self.ftype)
	self.permPath = path.Join(stPath, self.year, self.month, self.day, self.domain, self.user, self.fdir)
	return self
}

type DownloadManager struct {
	bucket       string
	downloadPath string
	storagePath  string
	keyRE        *regexp.Regexp
	mailer       *manager.Mailer
}

func NewDownloadManager(bucket string, downloadPath string, storagePath string, mailer *manager.Mailer) *DownloadManager {
	self := &DownloadManager{}
	self.bucket = bucket
	self.downloadPath = downloadPath
	self.storagePath = storagePath
	self.keyRE = regexp.MustCompile("^(\\d{4})-(\\d{2})-(\\d{2})-(.*)@(.*)-([A-Z0-9]{5,6})-(.*)$")
	self.mailer = mailer
	return self
}

func (self *DownloadManager) StartDownloadLoop() {
	go func() {
		for {
			self.download()
			time.Sleep(time.Minute * time.Duration(DOWNLOAD_INTERVAL))
		}
	}()
}

func (self *DownloadManager) download() {

	// ListObjects in our bucket
	response, err := s3.New(nil).ListObjects(&s3.ListObjectsInput{
		Bucket: aws.String(self.bucket),
		Prefix: aws.String("2"),
	})
	if err == nil {
		core.LogInfo("download", "ListObjects returned %d objects", len(response.Contents))
	} else {
		core.LogError(err, "download", "ListObjects failed")
		return
	}

	fetchedList := []*Download{}

	// Iterate over all returned objects
	for _, object := range response.Contents {
		key := *object.Key
		size := uint64(*object.Size)

		if size > DOWNLOAD_MAXIMUM {
			core.LogInfo("download", "Too large %d: %s", size, key)
			continue
		}

		// Parse the object key string, must be in expected format generated by miramar
		subs := self.keyRE.FindStringSubmatch(key)
		if len(subs) != 8 {
			core.LogInfo("download", "Failed to parse key: %s", key)
			continue
		}

		// Create download object
		d := NewDownload(self.storagePath, size, subs[1], subs[2], subs[3], subs[4], subs[5], subs[6], subs[7])

		// Skip if permPath already exists
		if _, err := os.Stat(d.permPath); err == nil {
			//core.LogInfo("download", "    Already in permanent storage, skipping")
			continue
		}

		core.LogInfo("download", "Processing %s", key)

		// Setup the local file
		downloadedFile := path.Join(self.downloadPath, key)
		fd, err := os.Create(downloadedFile)
		if err != nil {
			core.LogError(err, "download", "    Could not create file for download")
			continue
		}
		numBytes, err := s3manager.NewDownloader(nil).Download(fd,
			&s3.GetObjectInput{Bucket: &self.bucket, Key: &key})
		core.LogInfo("download", "    Downloaded %d bytes", numBytes)

		// Read 512 bytes of downloaded file for MIME type detection
		fd.Seek(0, 0)
		var magic []byte
		magic = make([]byte, 512)
		_, err = fd.Read(magic)
		fd.Close()
		if err != nil {
			core.LogError(err, "download", "    Could not read from downloaded file")
			continue
		}
		mimeType := http.DetectContentType(magic)

		// Handling of downloaded file depends on type
		var cmd *exec.Cmd
		if strings.HasPrefix(mimeType, "application/x-gzip") {
			core.LogInfo("download", "    Tar file, untar'ing")
			cmd = exec.Command("tar", "xf", downloadedFile, "-C", d.permPath)
		} else if strings.HasPrefix(mimeType, "text/plain") {
			core.LogInfo("download", "    Text file, copying")
			cmd = exec.Command("cp", downloadedFile, path.Join(d.permPath, d.fname))
		} else {
			core.LogInfo("download", "    Unknown file, copying")
			cmd = exec.Command("cp", downloadedFile, path.Join(d.permPath, d.fname))
		}

		// Create permanent storage folder for this key
		if err := os.MkdirAll(d.permPath, 0755); err != nil {
			core.LogError(err, "download", "    Could not create directory: %s", d.permPath)
			continue
		}

		// Execute handler command
		if _, err = cmd.Output(); err != nil {
			core.LogError(err, "download", "    Error while running handler")

			// Remove the permPath so this can be retried later
			os.RemoveAll(d.permPath)
			continue
		}

		// Success! Remove the temporary downloaded file
		os.Remove(downloadedFile)
		core.LogInfo("download", "    Handler complete, removed download file")
		fetchedList = append(fetchedList, d)
	}

	// Send out email enumerating newly downloaded files
	if len(fetchedList) > 0 {
		results := []string{}
		for i, d := range fetchedList {
			results = append(results, fmt.Sprintf("%d. %s@%s  %s  (%s)", i+1, d.user, d.domain, d.fname, humanize.Bytes(d.size)))
		}
		subj := fmt.Sprintf("%d New Customer Uploads", len(fetchedList))
		body := strings.Join(results, "\n")

		users := []string{}
		self.mailer.Sendmail(users, subj, body)
	}
}
