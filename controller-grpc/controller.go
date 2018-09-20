//go:generate protoc -I/usr/local/include -I../controller/grpc -I${GOPATH}/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis --go_out=plugins=grpc:. ../controller/grpc/controller.proto
package main

import (
	fmt "fmt"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	controller "github.com/flynn/flynn/controller/client"
	ct "github.com/flynn/flynn/controller/types"
	"github.com/flynn/flynn/host/resource"
	"github.com/flynn/flynn/host/types"
	"github.com/flynn/flynn/pkg/cors"
	"github.com/flynn/flynn/pkg/httphelper"
	"github.com/flynn/flynn/pkg/shutdown"
	proto "github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	any "github.com/golang/protobuf/ptypes/any"
	durpb "github.com/golang/protobuf/ptypes/duration"
	tspb "github.com/golang/protobuf/ptypes/timestamp"
	"github.com/improbable-eng/grpc-web/go/grpcweb"
	"github.com/opencontainers/runc/libcontainer/configs"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func mustEnv(key string) string {
	if val, ok := os.LookupEnv(key); ok {
		return val
	}
	panic(fmt.Errorf("%s is required", key))
}

func main() {
	client, err := controller.NewClient(mustEnv("CONTROLLER_DOMAIN"), mustEnv("CONTROLLER_AUTH_KEY"))
	if err != nil {
		shutdown.Fatal(fmt.Errorf("error initializing controller client: %s", err))
	}
	s := NewServer(&Config{
		Client: client,
	})

	wrappedServer := grpcweb.WrapServer(s)

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	addr := ":" + port
	shutdown.Fatal(
		http.ListenAndServe(
			addr,
			httphelper.ContextInjector(
				"controller-grpc",
				httphelper.NewRequestLogger(corsHandler(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					if wrappedServer.IsGrpcWebRequest(req) {
						wrappedServer.ServeHttp(w, req)
					}
					// Fall back to other servers.
					http.DefaultServeMux.ServeHTTP(w, req)
				}))),
			),
		),
	)
}

type Config struct {
	Client controller.Client
}

func corsHandler(main http.Handler) http.Handler {
	return (&cors.Options{
		ShouldAllowOrigin: func(origin string, req *http.Request) bool {
			return true
		},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD"},
		AllowHeaders:     []string{"Authorization", "Accept", "Content-Type", "If-Match", "If-None-Match", "X-GRPC-Web"},
		ExposeHeaders:    []string{"ETag"},
		AllowCredentials: true,
		MaxAge:           time.Hour,
	}).Handler(main)
}

func NewServer(c *Config) *grpc.Server {
	s := grpc.NewServer()
	RegisterControllerServer(s, &server{Config: c})
	// Register reflection service on gRPC server.
	reflection.Register(s)
	return s
}

type server struct {
	ControllerServer
	*Config
}

func convertApp(a *ct.App) *App {
	return &App{
		Name:          path.Join("apps", a.ID),
		DisplayName:   a.Name,
		Labels:        a.Meta,
		Strategy:      a.Strategy,
		Release:       path.Join("apps", a.ID, "releases", a.ReleaseID),
		DeployTimeout: a.DeployTimeout,
		CreateTime:    timestampProto(a.CreatedAt),
		UpdateTime:    timestampProto(a.UpdatedAt),
	}
}

func (s *server) ListApps(ctx context.Context, req *ListAppsRequest) (*ListAppsResponse, error) {
	ctApps, err := s.Client.AppList()
	if err != nil {
		return nil, err
	}
	apps := make([]*App, len(ctApps))
	for i, a := range ctApps {
		apps[i] = convertApp(a)
	}
	return &ListAppsResponse{
		Apps:          apps,
		NextPageToken: "", // there is no pagination
	}, nil
}

func (s *server) GetApp(ctx context.Context, req *GetAppRequest) (*App, error) {
	ctApp, err := s.Client.GetApp(parseResourceName(req.Name)["apps"])
	if err != nil {
		return nil, err
	}
	return convertApp(ctApp), nil
}

func backConvertApp(a *App) *ct.App {
	return &ct.App{
		ID:            parseResourceName(a.Name)["apps"],
		Name:          a.DisplayName,
		Meta:          a.Labels,
		Strategy:      a.Strategy,
		ReleaseID:     parseResourceName(a.Release)["releases"],
		DeployTimeout: a.DeployTimeout,
		CreatedAt:     timestampFromProto(a.CreateTime),
		UpdatedAt:     timestampFromProto(a.UpdateTime),
	}
}

func (s *server) UpdateApp(ctx context.Context, req *UpdateAppRequest) (*App, error) {
	ctApp := backConvertApp(req.App)
	if err := s.Client.UpdateApp(ctApp); err != nil {
		return nil, err
	}
	return convertApp(ctApp), nil
}

func convertPorts(from []ct.Port) []*Port {
	to := make([]*Port, len(from))
	for i, p := range from {
		to[i] = &Port{
			Port:    int32(p.Port),
			Proto:   p.Proto,
			Service: convertService(p.Service),
		}
	}
	return to
}

func backConvertPorts(from []*Port) []ct.Port {
	to := make([]ct.Port, len(from))
	for i, p := range from {
		to[i] = ct.Port{
			Port:    int(p.Port),
			Proto:   p.Proto,
			Service: backConvertService(p.Service),
		}
	}
	return to
}

func convertService(from *host.Service) *HostService {
	// TODO(jvatic)
	return &HostService{}
}

func backConvertService(from *HostService) *host.Service {
	// TODO(jvatic)
	return &host.Service{}
}

func convertVolumes(from []ct.VolumeReq) []*VolumeReq {
	// TODO(jvatic)
	return []*VolumeReq{}
}

func backConvertVolumes(from []*VolumeReq) []ct.VolumeReq {
	// TODO(jvatic)
	return []ct.VolumeReq{}
}

func convertResources(from resource.Resources) map[string]*HostResourceSpec {
	// TODO(jvatic)
	return map[string]*HostResourceSpec{}
}

func backConvertResources(from map[string]*HostResourceSpec) resource.Resources {
	// TODO(jvatic)
	return resource.Resources{}
}

func convertMounts(from []host.Mount) []*HostMount {
	// TODO(jvatic)
	return []*HostMount{}
}

func backConvertMounts(from []*HostMount) []host.Mount {
	// TODO(jvatic)
	return []host.Mount{}
}

func convertAllowedDevices(from []*configs.Device) []*LibContainerDevice {
	// TODO(jvatic)
	return []*LibContainerDevice{}
}

func backConvertAllowedDevices(from []*LibContainerDevice) []*configs.Device {
	// TODO(jvatic)
	return []*configs.Device{}
}

func convertProcesses(from map[string]ct.ProcessType) map[string]*ProcessType {
	to := make(map[string]*ProcessType, len(from))
	for k, t := range from {
		to[k] = &ProcessType{
			Args:              t.Args,
			Env:               t.Env,
			Ports:             convertPorts(t.Ports),
			Volumes:           convertVolumes(t.Volumes),
			Omni:              t.Omni,
			HostNetwork:       t.HostNetwork,
			HostPidNamespace:  t.HostPIDNamespace,
			Service:           t.Service,
			Resurrect:         t.Resurrect,
			Resources:         convertResources(t.Resources),
			Mounts:            convertMounts(t.Mounts),
			LinuxCapabilities: t.LinuxCapabilities,
			AllowedDevices:    convertAllowedDevices(t.AllowedDevices),
			WriteableCgroups:  t.WriteableCgroups,
		}
	}
	return to
}

func backConvertProcesses(from map[string]*ProcessType) map[string]ct.ProcessType {
	to := make(map[string]ct.ProcessType, len(from))
	for k, t := range from {
		to[k] = ct.ProcessType{
			Args:              t.Args,
			Env:               t.Env,
			Ports:             backConvertPorts(t.Ports),
			Volumes:           backConvertVolumes(t.Volumes),
			Omni:              t.Omni,
			HostNetwork:       t.HostNetwork,
			HostPIDNamespace:  t.HostPidNamespace,
			Service:           t.Service,
			Resurrect:         t.Resurrect,
			Resources:         backConvertResources(t.Resources),
			Mounts:            backConvertMounts(t.Mounts),
			LinuxCapabilities: t.LinuxCapabilities,
			AllowedDevices:    backConvertAllowedDevices(t.AllowedDevices),
			WriteableCgroups:  t.WriteableCgroups,
		}
	}
	return to
}

func convertRelease(r *ct.Release) *Release {
	return &Release{
		Name:       fmt.Sprintf("apps/%s/releases/%s", r.AppID, r.ID),
		Artifacts:  r.ArtifactIDs,
		Env:        r.Env,
		Labels:     r.Meta,
		Processes:  convertProcesses(r.Processes),
		CreateTime: timestampProto(r.CreatedAt),
	}
}

func (s *server) GetRelease(ctx context.Context, req *GetReleaseRequest) (*Release, error) {
	release, err := s.Client.GetRelease(parseResourceName(req.Name)["releases"])
	if err != nil {
		return nil, err
	}
	return convertRelease(release), nil
}

func (s *server) ListReleases(ctx context.Context, req *ListReleasesRequest) (*ListReleasesResponse, error) {
	appID := parseResourceName(req.Parent)["apps"]
	var ctReleases []*ct.Release
	if appID == "" {
		res, err := s.Client.ReleaseList()
		if err != nil {
			return nil, err
		}
		ctReleases = res
	} else {
		res, err := s.Client.AppReleaseList(appID)
		if err != nil {
			return nil, err
		}
		ctReleases = res
	}

	releases := make([]*Release, len(ctReleases))
	for i, r := range ctReleases {
		releases[i] = convertRelease(r)
	}
	return &ListReleasesResponse{Releases: releases}, nil
}

func (s *server) StreamAppLog(*StreamAppLogRequest, Controller_StreamAppLogServer) error {
	return nil
}

func (s *server) CreateRelease(ctx context.Context, req *CreateReleaseRequest) (*Release, error) {
	r := req.Release
	ctRelease := &ct.Release{
		ArtifactIDs: r.Artifacts,
		Env:         r.Env,
		Meta:        r.Labels,
		Processes:   backConvertProcesses(r.Processes),
	}
	if err := s.Client.CreateRelease(parseResourceName(req.Parent)["apps"], ctRelease); err != nil {
		return nil, err
	}
	return convertRelease(ctRelease), nil
}

func convertDeploymentTags(from map[string]map[string]string) map[string]*DeploymentProcessTags {
	to := make(map[string]*DeploymentProcessTags, len(from))
	for k, v := range from {
		to[k] = &DeploymentProcessTags{Tags: v}
	}
	return to
}

func convertDeploymentProcesses(from map[string]int) map[string]int32 {
	to := make(map[string]int32, len(from))
	for k, v := range from {
		to[k] = int32(v)
	}
	return to
}

func convertDeploymentStatus(from string) Deployment_Status {
	switch from {
	case "pending":
		return Deployment_PENDING
	case "failed":
		return Deployment_FAILED
	case "running":
		return Deployment_RUNNING
	case "complete":
		return Deployment_COMPLETE
	}
	return Deployment_PENDING
}

func convertDeployment(from *ct.Deployment) *Deployment {
	return &Deployment{
		Name:          fmt.Sprintf("apps/%s/deployments/%s", from.AppID, from.ID),
		OldRelease:    fmt.Sprintf("apps/%s/releases/%s", from.AppID, from.OldReleaseID),
		NewRelease:    fmt.Sprintf("apps/%s/releases/%s", from.AppID, from.NewReleaseID),
		Strategy:      from.Strategy,
		Status:        convertDeploymentStatus(from.Status),
		Processes:     convertDeploymentProcesses(from.Processes),
		Tags:          convertDeploymentTags(from.Tags),
		DeployTimeout: from.DeployTimeout,
		CreateTime:    timestampProto(from.CreatedAt),
		EndTime:       timestampProto(from.FinishedAt),
	}
}

func (s *server) CreateDeployment(req *CreateDeploymentRequest, ds Controller_CreateDeploymentServer) error {
	d, err := s.Client.CreateDeployment(parseResourceName(req.Parent)["apps"], parseResourceName(req.Release)["releases"])
	if err != nil {
		return err
	}
	events := make(chan *ct.Event)
	eventStream, err := s.Client.StreamEvents(ct.StreamEventsOptions{
		AppID:       d.AppID,
		ObjectID:    d.ID,
		ObjectTypes: []ct.EventType{ct.EventTypeDeployment},
		Past:        true,
	}, events)
	if err != nil {
		return err
	}

	for {
		ctEvent, ok := <-events
		if !ok {
			break
		}
		if ctEvent.ObjectType != "deployment" {
			continue
		}
		d, err := s.Client.GetDeployment(ctEvent.ObjectID)
		if err != nil {
			fmt.Printf("Failed to get deployment(%s): %s\n", ctEvent.ObjectID, err)
			continue
		}
		serializedDeployment, err := proto.Marshal(convertDeployment(d))
		if err != nil {
			fmt.Printf("Failed to serialize deployment deployment(%s): %s\n", d.ID, err)
			continue
		}
		ds.Send(&Event{
			Name:   fmt.Sprintf("events/%d", ctEvent.ID),
			Parent: fmt.Sprintf("apps/%s/deployments/%s", d.AppID, d.ID),
			Data: &any.Any{
				TypeUrl: fmt.Sprintf("apps/%s/deployments/%s", d.AppID, d.ID),
				Value:   serializedDeployment,
			},
		})
		if d.Status == "complete" || d.Status == "failed" {
			break
		}
	}

	if err := eventStream.Close(); err != nil {
		return err
	}

	return eventStream.Err()
}

func (s *server) StreamEvents(*StreamEventsRequest, Controller_StreamEventsServer) error {
	return nil
}

func parseResourceName(name string) map[string]string {
	parts := strings.Split(name, "/")
	idMap := make(map[string]string, len(parts)/2)
	for i := 0; i < len(parts)-1; i += 2 {
		if i == len(parts) {
			return idMap
		}
		resourceName := parts[i]
		resourceID := parts[i+1]
		idMap[resourceName] = resourceID
	}
	return idMap
}

func parseProtoDuration(dur *durpb.Duration) time.Duration {
	d, _ := ptypes.Duration(dur)
	return d
}

func timestampProto(t *time.Time) *tspb.Timestamp {
	if t == nil {
		return nil
	}
	tp, _ := ptypes.TimestampProto(*t)
	return tp
}

func timestampFromProto(t *tspb.Timestamp) *time.Time {
	if t == nil {
		return nil
	}
	ts, _ := ptypes.Timestamp(t)
	return &ts
}
