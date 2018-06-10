package main

import (
	"fmt"
	"html/template"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"syscall"
)

type Watches []*Watch

func (ws Watches) Len() int {
	return len(ws)
}

func (ws Watches) Swap(i, j int) {
	ws[i], ws[j] = ws[j], ws[i]
}

func (ws Watches) Less(i, j int) bool {
	return ws[i].Less(*ws[j])
}

func (ws *Watches) AddWatch(w *Watch) {
	*ws = append(*ws, w)
	sort.Stable(*ws)
}

func (ws Watches) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	if len(path) > 1 && path[0] == '/' {
		path = path[1:]
		for i := range ws {
			if ws[i].Endpoint == path {
				ws[i].ServeHttp(w, r)
				return
			}
		}
		http.NotFound(w, r)
		return
	}

	UI(w, r)
}

var allWatches Watches
var serverConfig Config

func UI(w http.ResponseWriter, r *http.Request) {
	t, err := template.New("status").Parse(`<!DOCTYPE html>
	<html><head>
	<title>Woof!</title>
	<style>
	h1 {text-align: center}
	table {width: 80%; margin: auto}
	th {text-align: left; background: #D0D0D0}     
	td, th {padding: 0.2em}
    	tr:nth-child(even) td {background: #F0F0F0}           
    	tr:nth-child(odd) td {background: #FDFDFD}
	.ttavail {
		position: relative;
		display: inline-block;
		border-bottom: 1px dotted black;
	}
	.ttavail .tt {
		visibility: hidden;
		background-color: black;
		color: #fff;
		text-align: center;
		border-radius: 6px;
		padding: 0.2em;
		top: 1.4em;
		left: 0;
		position: absolute;
	        z-index: 1;
	}
	.ttavail:hover .tt {visibility: visible}
	.late {
		color: red;
		font-weight: bold;
	}
	</style>
	<meta http-equiv="refresh" content="30">
	</head><body>
	<h1>Woof on {{.Cfg.ListenAddress}}</h1>
	<table>
	<thead><tr><th>Endpoint</th><th>Trigger</th><th>Interval</th><th>Last seen</th><th>Next expected</th></tr></thead>
	<tbody>{{ range .W }}
	  <tr>
	    <td>{{.Endpoint}}</td>

	    <td>{{if eq .Trigger 1 -}}
	     <form action="{{.Endpoint}}" method="POST">
	       <input type="submit" value="{{.Trigger}}"/>
	     </form>
	    {{- else -}}
	      {{.Trigger}}
	    {{- end}}</td>

	    <td><div class="ttavail">{{.Interval}}<span class="tt">{{.StatsFriendly}}</span></div></td>

	    <td>{{if gt .MissedReports 0 -}}
	      <span class="late">{{.LastSeenFriendly}}</span>
	    {{- else -}}
	      {{.LastSeenFriendly}}
	    {{- end}}</td>

	    <td>{{.DueFriendly }}</td>
	  </tr>
	{{ end }}</tbody>
	</table>
	</body></html>
	`)

	if err != nil {
		http.Error(w, "500 Internal server fault", 500)
		return
	}

	sort.Sort(allWatches)
	err = t.Execute(w, struct {
		Cfg Config
		W   []*Watch
	}{serverConfig, allWatches})
	if err != nil {
		http.Error(w, "500 Internal server fault", 500)
	}
}

func loadCfg(config string) error {
	myConfig, err := loadConfig(config)
	if err != nil {
		return err
	}
	serverConfig = myConfig.ServerConfig

	var endpointStatus StatusFile
	if serverConfig.StateFile != "" {
		endpointStatus, err = loadStatus(serverConfig.StateFile)
		if err != nil {
			fmt.Println(serverConfig.StateFile, err, ", continuing without previous state")
		}
	}

	pid := os.Getpid()
	for k, v := range myConfig.Watches {
		lastState, _ := endpointStatus[k]
		myWatch := NewWatch(k, v, lastState, serverConfig.ExecArgs, pid)
		allWatches.AddWatch(myWatch)
	}

	if len(allWatches) == 0 {
		return fmt.Errorf("No watches configured (malformed config file?)")
	}

	return nil
}

func main() {
	if len(os.Args) > 2 {
		fmt.Println("Usage:", os.Args[0], "[cfg-file.json]")
		os.Exit(1)
	}

	cfgFile := "config.json"
	if len(os.Args) == 2 {
		cfgFile = os.Args[1]
	}
	if err := loadCfg(cfgFile); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	if serverConfig.StateFile != "" {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGHUP)
		go func(c <-chan os.Signal) {
			for _ = range c {
				saveStatus(serverConfig.StateFile, allWatches)
			}
		}(c)
	}

	if err := http.ListenAndServe(serverConfig.ListenAddress, allWatches); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
