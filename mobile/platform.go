//go:build android || linux

package tainacore

import (
	"context"
	"fmt"
	"net/netip"
	"os"
	"syscall"
	"unsafe"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/option"
	tun "github.com/sagernet/sing-tun"
	"github.com/sagernet/sing/common/control"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/sing/common/logger"
	"github.com/sagernet/sing/common/x/list"
	"golang.org/x/sys/unix"
)

// platform implements sing-box's adapter.PlatformInterface for Android:
// the TUN device is created by VpnService on the Kotlin side and every
// outgoing socket is protected so it bypasses the VPN.
var _ adapter.PlatformInterface = (*platform)(nil)

type platform struct {
	host Host
}

func (s *platform) Initialize(networkManager adapter.NetworkManager) error {
	return nil
}

func (s *platform) UsePlatformAutoDetectInterfaceControl() bool {
	return true
}

func (s *platform) AutoDetectInterfaceControl(fd int) error {
	if !s.host.Protect(int32(fd)) {
		return E.New("failed to protect socket")
	}
	return nil
}

func (s *platform) UsePlatformInterface() bool {
	return true
}

func (s *platform) OpenInterface(options *tun.Options, platformOptions option.TunPlatformOptions) (tun.Tun, error) {
	tunFd, err := s.host.OpenTun()
	if err != nil {
		return nil, E.Cause(err, "open tun")
	}
	name, err := getTunnelName(tunFd)
	if err != nil {
		name = "tun0"
	}
	options.Name = name
	if options.InterfaceMonitor != nil {
		options.InterfaceMonitor.RegisterMyInterface(name)
	}
	dupFd, err := syscall.Dup(int(tunFd))
	if err != nil {
		return nil, E.Cause(err, "dup tun file descriptor")
	}
	options.FileDescriptor = dupFd
	return tun.New(*options)
}

func (s *platform) ProcessPlatformOptions(options option.TunPlatformOptions) error {
	return nil
}

func (s *platform) UsePlatformDefaultInterfaceMonitor() bool {
	return true
}

func (s *platform) CreateDefaultInterfaceMonitor(logger logger.Logger) tun.DefaultInterfaceMonitor {
	return &interfaceMonitor{}
}

func (s *platform) UsePlatformNetworkInterfaces() bool {
	return false
}

func (s *platform) NetworkInterfaces() ([]adapter.NetworkInterface, error) {
	return nil, os.ErrInvalid
}

func (s *platform) UnderNetworkExtension() bool {
	return false
}

func (s *platform) NetworkExtensionIncludeAllNetworks() bool {
	return false
}

func (s *platform) ClearDNSCache() {
}

func (s *platform) RequestPermissionForWIFIState() error {
	return nil
}

func (s *platform) UsePlatformWIFIMonitor() bool {
	return false
}

func (s *platform) ReadWIFIState(ctx context.Context) adapter.WIFIState {
	return adapter.WIFIState{}
}

func (s *platform) UsePlatformConnectionOwnerFinder() bool {
	return false
}

func (s *platform) FindConnectionOwner(request *adapter.FindConnectionOwnerRequest) (*adapter.ConnectionOwner, error) {
	return nil, os.ErrInvalid
}

func (s *platform) UsePlatformNotification() bool {
	return false
}

func (s *platform) SendNotification(notification *adapter.Notification) error {
	return nil
}

func (s *platform) CancelNotification(identifier string, typeID int32) error {
	return nil
}

func (s *platform) MyInterfaceAddress() []netip.Addr {
	return nil
}

func (s *platform) UsePlatformNeighborResolver() bool {
	return false
}

func (s *platform) StartNeighborMonitor(listener adapter.NeighborUpdateListener) error {
	return os.ErrInvalid
}

func (s *platform) CloseNeighborMonitor(listener adapter.NeighborUpdateListener) error {
	return nil
}

func (s *platform) UsePlatformShell() bool {
	return false
}

func (s *platform) CheckPlatformShell() error {
	return nil
}

func (s *platform) OpenShellSession(user *adapter.PlatformUser, command string, env []string, term string, rows int32, cols int32) (adapter.ShellSession, error) {
	return nil, os.ErrInvalid
}

func (s *platform) LookupSFTPServer() (string, error) {
	return "", os.ErrInvalid
}

func (s *platform) ReadSystemSSHHostKey() ([]byte, error) {
	return nil, os.ErrInvalid
}

func (s *platform) TailscaleHostname() string {
	return ""
}

func (s *platform) UsePlatformBridge() bool {
	return false
}

func (s *platform) CreateBridge(options adapter.BridgeOptions) (adapter.BridgeSession, error) {
	return nil, os.ErrInvalid
}

func (s *platform) LookupUser(username string) (*adapter.PlatformUser, error) {
	return nil, os.ErrInvalid
}

func (s *platform) UsePlatformLocalDNSTransport() bool {
	return false
}

func (s *platform) LocalDNSTransport() dns.TransportConstructorFunc[option.LocalDNSServerOptions] {
	return nil
}

type interfaceMonitor struct{}

func (s *interfaceMonitor) Start() error {
	return nil
}

func (s *interfaceMonitor) Close() error {
	return nil
}

func (s *interfaceMonitor) DefaultInterface() *control.Interface {
	return nil
}

func (s *interfaceMonitor) OverrideAndroidVPN() bool {
	return false
}

func (s *interfaceMonitor) AndroidVPNEnabled() bool {
	return false
}

func (s *interfaceMonitor) RegisterCallback(callback tun.DefaultInterfaceUpdateCallback) *list.Element[tun.DefaultInterfaceUpdateCallback] {
	return nil
}

func (s *interfaceMonitor) UnregisterCallback(element *list.Element[tun.DefaultInterfaceUpdateCallback]) {
}

func (s *interfaceMonitor) RegisterMyInterface(interfaceName string) {
}

func (s *interfaceMonitor) MyInterfaces() []string {
	return nil
}

const ifReqSize = unix.IFNAMSIZ + 64

func getTunnelName(fd int32) (string, error) {
	var ifr [ifReqSize]byte
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TUNGETIFF), uintptr(unsafe.Pointer(&ifr[0])))
	if errno != 0 {
		return "", fmt.Errorf("failed to get name of TUN device: %w", errno)
	}
	return unix.ByteSliceToString(ifr[:]), nil
}
