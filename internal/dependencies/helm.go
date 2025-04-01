package dependencies

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v2"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/cli/values"
	"helm.sh/helm/v3/pkg/getter"
	"helm.sh/helm/v3/pkg/repo"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// HelmChartOptions defines options for installing a Helm chart
type HelmChartOptions struct {
	RepoName       string
	RepoURL        string
	ChartName      string
	ReleaseName    string
	Namespace      string
	Version        string
	ValuesFile     string
	Timeout        time.Duration
	Wait           bool
	CreateNS       bool
	ValueOverrides map[string]interface{}
}

// InstallHelmChart installs a Helm chart with the given options
func (self *DependenciesManager) InstallHelmChart(ctx context.Context, opts HelmChartOptions) error {
	self.logf("Adding Helm repository %s at %s", opts.RepoName, opts.RepoURL)

	// Add helm repo
	if err := self.addHelmRepo(opts.RepoName, opts.RepoURL); err != nil {
		return fmt.Errorf("failed to add helm repo: %w", err)
	}

	// Update helm repos
	if err := self.updateHelmRepos(); err != nil {
		return fmt.Errorf("failed to update helm repos: %w", err)
	}

	// Install the chart
	self.logf("Installing Helm chart %s in namespace %s", opts.ChartName, opts.Namespace)
	if err := self.installChart(opts); err != nil {
		return fmt.Errorf("failed to install helm chart: %w", err)
	}

	self.logf("Successfully installed %s in namespace %s", opts.ChartName, opts.Namespace)
	return nil
}

// addHelmRepo adds a Helm repository
func (self *DependenciesManager) addHelmRepo(name, url string) error {
	repoFile := self.helmEnv.RepositoryConfig

	// Create the repo file if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(repoFile), 0755); err != nil {
		return err
	}

	// Initialize the repo file
	b, err := os.ReadFile(repoFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	var f repo.File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return err
	}

	// Check if the repo already exists
	for _, r := range f.Repositories {
		if r.Name == name {
			// Repository already exists
			return nil
		}
	}

	// Add the repo
	r := &repo.Entry{
		Name: name,
		URL:  url,
	}

	f.Repositories = append(f.Repositories, r)

	// Save the repository file
	data, err := yaml.Marshal(f)
	if err != nil {
		return err
	}

	return os.WriteFile(repoFile, data, 0644)
}

// updateHelmRepos updates all Helm repositories
func (self *DependenciesManager) updateHelmRepos() error {
	repoFile := self.helmEnv.RepositoryConfig

	b, err := os.ReadFile(repoFile)
	if err != nil {
		return err
	}

	var f repo.File
	if err := yaml.Unmarshal(b, &f); err != nil {
		return err
	}

	for _, r := range f.Repositories {
		self.logf("Updating Helm repository %s", r.Name)

		// Create a repository client
		chartRepo, err := repo.NewChartRepository(r, getter.All(self.helmEnv))
		if err != nil {
			return err
		}

		// Download the index file
		if _, err := chartRepo.DownloadIndexFile(); err != nil {
			return fmt.Errorf("failed to download index file for repository %s: %w", r.Name, err)
		}
	}

	return nil
}

// installChart installs a Helm chart
func (self *DependenciesManager) installChart(opts HelmChartOptions) error {
	// Create the namespace if it doesn't exist
	if opts.CreateNS {
		if err := self.createNamespaceIfNotExists(opts.Namespace); err != nil {
			return err
		}
	}

	// Setup Helm client
	actionConfig := new(action.Configuration)
	if err := actionConfig.Init(self.helmEnv.RESTClientGetter(), opts.Namespace, "", self.logf); err != nil {
		return fmt.Errorf("failed to initialize helm action config: %w", err)
	}

	// Create install client
	client := action.NewInstall(actionConfig)
	client.Namespace = opts.Namespace
	client.ReleaseName = opts.ReleaseName
	client.CreateNamespace = opts.CreateNS
	client.Wait = opts.Wait
	client.Timeout = opts.Timeout

	if opts.Version != "" {
		client.Version = opts.Version
	}

	// Get chart values
	valueOpts := &values.Options{}
	if opts.ValuesFile != "" {
		valueOpts.ValueFiles = []string{opts.ValuesFile}
	}

	vals, err := valueOpts.MergeValues(getter.All(self.helmEnv))
	if err != nil {
		return fmt.Errorf("failed to merge values: %w", err)
	}

	// Apply any overrides
	for k, v := range opts.ValueOverrides {
		vals[k] = v
	}

	// Locate the chart
	chartPath, err := client.ChartPathOptions.LocateChart(opts.ChartName, self.helmEnv)
	if err != nil {
		return fmt.Errorf("failed to locate chart: %w", err)
	}

	// Load the chart
	chart, err := loader.Load(chartPath)
	if err != nil {
		return fmt.Errorf("failed to load chart: %w", err)
	}

	// Install the chart
	_, err = client.Run(chart, vals)
	return err
}

// createNamespaceIfNotExists creates a namespace if it doesn't exist
func (self *DependenciesManager) createNamespaceIfNotExists(namespace string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := self.kubeClient.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		// Namespace already exists
		return nil
	}

	// Create the namespace
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	_, err = self.kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	return err
}
