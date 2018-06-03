package main

import (
	"encoding/json"
	"io/ioutil"
	"strings"
	"time"
)

// TriggerType describes what sorts of trigger are supported
type TriggerType int

const (
	Post TriggerType = iota
	Manual
	Periodic
)

func (t *TriggerType) UnmarshalJSON(data []byte) error {
	switch strings.ToLower(string(data)) {
	case "\"post\"":
		*t = Post
	case "\"manual\"":
		*t = Manual
	case "\"periodic\"":
		*t = Periodic
	default:
		*t = Manual // emit diagnostic?
	}
	return nil
}

func (t TriggerType) String() string {
	switch t {
	case Post:
		return "Post"
	case Manual:
		return "Manual"
	case Periodic:
		return "Periodic"
	}
	return "(unknown)"
}

// Duration wraps time.Duration to allow some custom formatters to
// be applied.
type Duration time.Duration

func (d Duration) MarshalText() ([]byte, error) {
	return []byte(time.Duration(d).String()), nil
}

func (d Duration) String() string {
	return time.Duration(d).String()
}

func (d *Duration) UnmarshalText(val []byte) error {
	if len(val) == 0 {
		return nil
	}

	duration, err := time.ParseDuration(string(val))
	if err != nil {
		return err
	}

	*d = Duration(duration)
	return nil
}

type ConfigFile struct {
	ServerConfig Config
	Watches      map[string]WatchDogConfig
}

type Config struct {
	ListenAddress string
	StateFile     string
	ExecArgs      []string
}

func loadConfig(file string) (ConfigFile, error) {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return ConfigFile{}, err
	}

	myConfig := ConfigFile{
		ServerConfig: Config{
			ListenAddress: "127.0.0.1:8080",
			ExecArgs:      []string{"/bin/bash", "-c"},
		},
	}
	if err := json.Unmarshal(data, &myConfig); err != nil {
		return ConfigFile{}, err
	}

	return myConfig, nil
}

type StatusFile map[string]WatchDogStatus

func loadStatus(file string) (StatusFile, error) {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return StatusFile{}, err
	}

	var myStatus StatusFile
	if err := json.Unmarshal(data, &myStatus); err != nil {
		return StatusFile{}, err
	}

	return myStatus, nil
}

func saveStatus(file string, statuses []*Watch) error {
	myStatus := make(StatusFile)
	for _, s := range statuses {
		if s.Endpoint != "" && !s.LastSeen().IsZero() {
			myStatus[s.Endpoint] = s.Status()
		}
	}
	f, err := json.MarshalIndent(myStatus, "", "  ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(file, f, 0644)
}
