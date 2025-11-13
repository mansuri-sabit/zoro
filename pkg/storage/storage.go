package storage

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type Driver interface {
	GetRecordingURL(callSID string) (string, error)
	DownloadRecording(callSID string, exotelURL string) error
}

type ExotelProxyDriver struct {
	exotelBaseURL string
}

func NewExotelProxyDriver(accountSID string) *ExotelProxyDriver {
	return &ExotelProxyDriver{
		exotelBaseURL: fmt.Sprintf("https://api.exotel.com/v1/Accounts/%s", accountSID),
	}
}

func (d *ExotelProxyDriver) GetRecordingURL(callSID string) (string, error) {
	if callSID == "" {
		return "", fmt.Errorf("callSID is required")
	}
	return fmt.Sprintf("%s/Calls/%s/Recording.mp3", d.exotelBaseURL, callSID), nil
}

func (d *ExotelProxyDriver) DownloadRecording(callSID string, exotelURL string) error {
	return nil
}

type LocalDriver struct {
	basePath string
}

func NewLocalDriver(basePath string) *LocalDriver {
	if basePath == "" {
		basePath = "/data/audio"
	}
	return &LocalDriver{basePath: basePath}
}

func (d *LocalDriver) GetRecordingURL(callSID string) (string, error) {
	if callSID == "" {
		return "", fmt.Errorf("callSID is required")
	}
	return fmt.Sprintf("/recordings/%s.mp3", callSID), nil
}

func (d *LocalDriver) DownloadRecording(callSID string, exotelURL string) error {
	if err := os.MkdirAll(d.basePath, 0755); err != nil {
		return fmt.Errorf("failed to create storage directory: %w", err)
	}

	resp, err := http.Get(exotelURL)
	if err != nil {
		return fmt.Errorf("failed to download recording: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download recording: status %d", resp.StatusCode)
	}

	filePath := filepath.Join(d.basePath, fmt.Sprintf("%s.mp3", callSID))
	file, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

func NewDriver(driverType string, accountSID string, localPath string) (Driver, error) {
	switch strings.ToLower(driverType) {
	case "exotel-proxy", "proxy":
		return NewExotelProxyDriver(accountSID), nil
	case "local":
		return NewLocalDriver(localPath), nil
	default:
		return nil, fmt.Errorf("unknown storage driver: %s", driverType)
	}
}
