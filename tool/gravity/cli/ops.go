package cli

import (
	"context"
	"encoding/json"
	"fmt"
	_ "net/http/pprof"
	"strings"

	"github.com/gravitational/gravity/lib/app/docker"
	appservice "github.com/gravitational/gravity/lib/app/service"
	"github.com/gravitational/gravity/lib/constants"
	"github.com/gravitational/gravity/lib/defaults"
	"github.com/gravitational/gravity/lib/install"
	"github.com/gravitational/gravity/lib/localenv"
	"github.com/gravitational/gravity/lib/pack"
	"github.com/gravitational/gravity/lib/pack/encryptedpack"
	"github.com/gravitational/gravity/lib/state"
	"github.com/gravitational/gravity/lib/storage"
	"github.com/gravitational/gravity/lib/users"
	"github.com/gravitational/gravity/lib/utils"
	"github.com/gravitational/gravity/tool/common"

	"github.com/gravitational/license"
	"github.com/gravitational/trace"
)

func selectNetworkInterface() (addr string, err error) {
	for {
		addr, err = selectInterface()
		if err != nil {
			return "", trace.Wrap(err)
		}
		fmt.Printf("confirm the config:\n\n* IP address: %v\n\n", addr)
		re, err := confirm()
		if err != nil {
			return "", trace.Wrap(err)
		}
		if !re {
			continue
		}
		break
	}
	return addr, nil
}

func mustJSON(i interface{}) string {
	bytes, err := json.Marshal(i)
	if err != nil {
		panic(err)
	}
	return string(bytes)
}

func appPackage(env *localenv.LocalEnvironment) error {
	apps, err := env.AppServiceLocal(localenv.AppConfig{})
	if err != nil {
		return trace.Wrap(err)
	}

	appPackage, err := install.GetAppPackage(apps)
	if err != nil {
		return trace.Wrap(err)
	}

	fmt.Printf("%v", appPackage)
	return nil
}

func uploadUpdate(env *localenv.LocalEnvironment, opsURL string) error {
	// create local environment with gravity state dir because the environment
	// provided above has upgrade tarball as a state dir
	localStateDir, err := localenv.LocalGravityDir()
	if err != nil {
		return trace.Wrap(err)
	}

	defaultEnv, err := localenv.New(localStateDir)
	if err != nil {
		return trace.Wrap(err)
	}

	clusterOperator, err := defaultEnv.SiteOperator()
	if err != nil {
		return trace.Wrap(err)
	}

	cluster, err := clusterOperator.GetLocalSite()
	if err != nil {
		return trace.Wrap(err)
	}

	var tarballPackages pack.PackageService = env.Packages
	if cluster.License != nil {
		parsed, err := license.ParseLicense(cluster.License.Raw)
		if err != nil {
			return trace.Wrap(err)
		}

		encryptionKey := parsed.GetPayload().EncryptionKey
		if len(encryptionKey) != 0 {
			tarballPackages = encryptedpack.New(tarballPackages, string(encryptionKey))
		}
	}

	clusterPackages, err := defaultEnv.ClusterPackages()
	if err != nil {
		return trace.Wrap(err)
	}

	clusterApps, err := defaultEnv.SiteApps()
	if err != nil {
		return trace.Wrap(err)
	}

	tarballApps, err := env.AppServiceLocal(localenv.AppConfig{
		Packages: tarballPackages,
	})
	if err != nil {
		return trace.Wrap(err)
	}

	appPackage, err := install.GetAppPackage(tarballApps)
	if err != nil {
		return trace.Wrap(err)
	}

	env.PrintStep("Importing application %v v%v", appPackage.Name, appPackage.Version)
	_, err = appservice.PullApp(appservice.AppPullRequest{
		SrcPack: tarballPackages,
		SrcApp:  tarballApps,
		DstPack: clusterPackages,
		DstApp:  clusterApps,
		Package: *appPackage,
	})
	if err != nil {
		if !trace.IsAlreadyExists(err) {
			return trace.Wrap(err)
		}
		env.PrintStep("Application already exists in local cluster")
	}

	var registries []string
	err = utils.Retry(defaults.RetryInterval, defaults.RetryLessAttempts, func() error {
		registries, err = getRegistries(context.TODO(), defaultEnv, cluster.ClusterState.Servers)
		return trace.Wrap(err)
	})
	if err != nil {
		return trace.Wrap(err)
	}

	stateDir, err := state.GetStateDir()
	if err != nil {
		return trace.Wrap(err)
	}

	for _, registry := range registries {
		env.PrintStep("Synchronizing application with Docker registry %v",
			registry)

		imageService, err := docker.NewImageService(docker.RegistryConnectionRequest{
			RegistryAddress: registry,
			CertName:        constants.DockerRegistry,
			CACertPath:      state.Secret(stateDir, defaults.RootCertFilename),
			ClientCertPath:  state.Secret(stateDir, "kubelet.cert"),
			ClientKeyPath:   state.Secret(stateDir, "kubelet.key"),
		})
		if err != nil {
			return trace.Wrap(err)
		}
		err = appservice.SyncApp(context.TODO(), appservice.SyncRequest{
			PackService:  clusterPackages,
			AppService:   clusterApps,
			ImageService: imageService,
			Package:      *appPackage,
		})
		if err != nil {
			return trace.Wrap(err)
		}
	}

	env.PrintStep("Application has been uploaded")
	return nil
}

// getRegistries returns a list of registry addresses in the cluster
func getRegistries(ctx context.Context, env *localenv.LocalEnvironment, servers []storage.Server) ([]string, error) {
	// in planets before certain version registry was running only on active master
	version, err := planetVersion(env)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	if version.LessThan(*constants.PlanetMultiRegistryVersion) {
		return []string{constants.DockerRegistry}, nil
	}
	// otherwise return registry addresses on all masters
	ips, err := getMasterNodes(ctx, servers)
	if err != nil {
		return nil, trace.Wrap(err)
	}
	registries := make([]string, 0, len(ips))
	for _, ip := range ips {
		registries = append(registries, defaults.DockerRegistryAddr(ip))
	}
	return registries, nil
}

// connectToOpsCenter
func connectToOpsCenter(env *localenv.LocalEnvironment, opsCenterURL, username, password string) (err error) {
	if username == "" || password == "" {
		username, password, err = common.ReadUserPass()
		if err != nil {
			return trace.Wrap(err)
		}
	}
	entry, err := env.Creds.UpsertLoginEntry(
		users.LoginEntry{
			OpsCenterURL: opsCenterURL,
			Email:        username,
			Password:     password})
	if err != nil {
		return trace.Wrap(err)
	}
	fmt.Printf("\n\nconnected to %v\n", *entry)
	return nil
}

// disconnectFromOpsCenter
func disconnectFromOpsCenter(env *localenv.LocalEnvironment, opsCenterURL string) error {
	err := env.Creds.DeleteLoginEntry(opsCenterURL)
	if err != nil && !trace.IsNotFound(err) {
		return trace.Wrap(err)
	}
	fmt.Printf("disconnected from %v", opsCenterURL)
	return nil
}

func listOpsCenters(env *localenv.LocalEnvironment) error {
	entries, err := env.Creds.GetLoginEntries()
	if err != nil {
		return trace.Wrap(err)
	}
	common.PrintHeader("logins")
	for _, entry := range entries {
		fmt.Printf("* %v %v\n", entry.OpsCenterURL, entry.Email)
	}
	fmt.Printf("\n")
	return nil
}

type envvars map[string]string

func newEnvironSource(env []string) (result envvars) {
	result = make(map[string]string)
	for _, variable := range env {
		keyvalue := strings.Split(variable, "=")
		if len(keyvalue) == 2 {
			key, value := keyvalue[0], keyvalue[1]
			result[key] = value
		}
	}
	return result
}

func (r envvars) GetEnv(name string) string {
	return r[name]
}
