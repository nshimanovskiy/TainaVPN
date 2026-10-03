// Package core wraps the sing-box library: it parses a JSON config,
// starts a box instance and keeps the last log lines for the UI.
package core

import (
	"context"
	"os"
	"strings"
	"sync"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/include"
	"github.com/sagernet/sing-box/log"
	"github.com/sagernet/sing-box/option"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/json"
	"github.com/sagernet/sing/service"
	"github.com/sagernet/sing/service/filemanager"
)

// Version of the embedded sing-box core.
func Version() string { return C.Version }

// Instance is a running sing-box.
type Instance struct {
	box    *box.Box
	cancel context.CancelFunc
}

// Start parses config and starts sing-box. platform may be nil (desktop/server).
// dataDir is where sing-box keeps its files (cache.db).
func Start(config string, dataDir string, platform adapter.PlatformInterface, logs *LogBuffer) (*Instance, error) {
	if dataDir != "" {
		_ = os.MkdirAll(dataDir, 0o700)
	}
	base := filemanager.WithDefault(context.Background(), dataDir, "", os.Getuid(), os.Getgid())
	ctx, cancel := context.WithCancel(include.Context(base))
	if platform != nil {
		ctx = service.ContextWith[adapter.PlatformInterface](ctx, platform)
	}
	options, err := json.UnmarshalExtendedContext[option.Options](ctx, []byte(config))
	if err != nil {
		cancel()
		return nil, E.Cause(err, "decode config")
	}
	boxOptions := box.Options{Context: ctx, Options: options}
	if logs != nil {
		boxOptions.PlatformLogWriter = logs
	}
	instance, err := box.New(boxOptions)
	if err != nil {
		cancel()
		return nil, E.Cause(err, "create service")
	}
	if err = instance.Start(); err != nil {
		instance.Close()
		cancel()
		return nil, E.Cause(err, "start service")
	}
	return &Instance{box: instance, cancel: cancel}, nil
}

// Close stops sing-box.
func (i *Instance) Close() error {
	if i == nil {
		return nil
	}
	err := i.box.Close()
	i.cancel()
	return err
}

// LogBuffer keeps the last N log lines.
type LogBuffer struct {
	mu    sync.Mutex
	lines []string
	max   int
	// Echo, if set, receives every line as it arrives (used by CLI tools).
	Echo func(string)
}

func NewLogBuffer(max int) *LogBuffer { return &LogBuffer{max: max} }

func (l *LogBuffer) WriteMessage(level log.Level, message string) {
	l.Add(message)
}

func (l *LogBuffer) Add(message string) {
	if l.Echo != nil {
		l.Echo(strings.TrimRight(message, "\n"))
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, strings.TrimRight(message, "\n"))
	if len(l.lines) > l.max {
		l.lines = l.lines[len(l.lines)-l.max:]
	}
}

func (l *LogBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}

func (l *LogBuffer) Clear() {
	l.mu.Lock()
	l.lines = nil
	l.mu.Unlock()
}
