package main

import (
	"fmt"
	"os"

	"io/ioutil"
	"path/filepath"

	"github.com/urfave/cli"
	"gopkg.in/yaml.v2"

	vaultapi "github.com/hashicorp/vault/api"
)

var (
	version = "Development"
)

// EnvErrorCode Exit Code for Missing Environment
const EnvErrorCode = 2

// YamlErrorCode Exit Code for YAML Errors
const YamlErrorCode = 5

// VaultErrorCode Exit Code for Vault Errors
const VaultErrorCode = 6

// VaultConfig is the configuration for connecting to a vault server.
type VaultConfig struct {
	Address string
	Token   string
	// SSL indicates we should use a secure connection while talking to Vault.
	SSL *SSLConfig
}

// SSLConfig is the configuration for SSL.
type SSLConfig struct {
	Enabled bool
	Verify  bool
	Cert    string
	CaCert  string
}

// GetVaultSecret - Pull a Secret From Vault given a path
func GetVaultSecret(path string) (*vaultapi.Secret, error) {
	// Get Config Completely From Environment
	var c *vaultapi.Config

	vault, err := vaultapi.NewClient(c)

	if err != nil {
		return nil, fmt.Errorf("Vault - Client Error: %s", err)
	}

	vaultSecret, err := vault.Logical().Read(path)

	if err != nil {
		return nil, fmt.Errorf("Vault - Read Error: %s", err)
	}
	if vaultSecret == nil {
		return nil, fmt.Errorf("Vault - No secret at path: %s", path)
	}
	return vaultSecret, nil
}

func main() {
	app := cli.NewApp()

	var env string
	var dc string
	var varsFile string
	var mlockBool = false

	type EnvVars map[string]string

	type Secrets map[string]string

	type ConfigV1 struct {
		Vars         EnvVars
		Secrets      Secrets
		Environments map[string]struct {
			Vars    EnvVars
			Secrets Secrets
			Dcs     map[string]EnvVars
		}
	}

	type Config struct {
		Vars         EnvVars
		Secrets      Secrets
		Environments map[string]struct {
			Vars    EnvVars
			Secrets Secrets
			Dcs     map[string]struct {
				Vars    EnvVars
				Secrets Secrets
			}
		}
	}

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "environment, e",
			Usage:       "Environment (qa, dev, stage, prod, etc)",
			EnvVar:      "ENVIRONMENT",
			Destination: &env,
		},
		cli.StringFlag{
			Name:        "datacenter, d",
			Usage:       "Datacenter (ndc_as_a, etc)",
			EnvVar:      "DATACENTER",
			Destination: &dc,
		},
		cli.StringFlag{
			Name:        "variables_file, f",
			Value:       "variables.yml",
			Usage:       "Variables YAML file",
			EnvVar:      "VARIABLES_FILE",
			Destination: &varsFile,
		},
		cli.BoolFlag{
			Name:        "mlock_enabled, m",
			Usage:       "Will attempt system mlock if set (prevent write to swap)",
			Required:    false,
			Destination: &mlockBool,
		},
	}

	app.Version = version
	app.Name = "buildenv"
	app.Usage = "Get the Build Environment from a settings yaml file."

	app.Action = func(c *cli.Context) error {

		enableMlock(mlockBool)

		if env == "" {
			return cli.NewExitError("environment is required", EnvErrorCode)
		}

		filename, _ := filepath.Abs(varsFile)
		yamlFile, err := ioutil.ReadFile(filename)

		if err != nil {
			return cli.NewExitError(fmt.Sprintf("unable to read variable file %s", varsFile), 4)
		}

		// Legacy
		var legacy = false
		var configV1 ConfigV1
		var config Config

		err = yaml.Unmarshal(yamlFile, &config)
		if err != nil {
			err = yaml.Unmarshal(yamlFile, &configV1)
			legacy = true

			if err != nil {
				fmt.Println(err)
				return cli.NewExitError("unable to unmarshal yaml", YamlErrorCode)
			}
		}

		fmt.Println("# Setting Variables for:")
		fmt.Printf("# Environment: %s\n", env)
		if dc != "" {
			fmt.Printf("# Datacenter: %s\n", dc)
		}

		// Print The Globals
		fmt.Println("# Global Vars:")
		for k, v := range config.Vars {
			fmt.Printf("export %s=%q\n", k, v)
		}

		fmt.Println("# Global Secrets:")
		for k, path := range config.Secrets {
			secret, err := GetVaultSecret(path)
			if err == nil {
				fmt.Printf("export %s=%q # %s\n", k, secret.Data["value"], path)
			} else {
				return cli.NewExitError(err.Error(), VaultErrorCode)
			}
		}

		// Print The Environment Specific Vars
		fmt.Printf("# Environment (%s) Vars:\n", env)
		for k, v := range config.Environments[env].Vars {
			fmt.Printf("export %s=%q\n", k, v)
		}

		fmt.Printf("# Environment (%s) Secrets:\n", env)
		for k, path := range config.Environments[env].Secrets {
			secret, err := GetVaultSecret(path)
			if err == nil {
				fmt.Printf("export %s=%q # %s\n", k, secret.Data["value"], path)
			} else {
				return cli.NewExitError(err.Error(), VaultErrorCode)
			}
		}

		// Print the DC Specific Vars
		if legacy {
			if dc != "" {
				fmt.Printf("# Datacenter (%s) Specific Vars:\n", dc)
				for k, v := range config.Environments[env].Dcs[dc].Vars {
					fmt.Printf("export %s=%q\n", k, v)
				}
			}
		} else {
			fmt.Printf("# Datacenter (%s) Specific Vars:\n", env)
			for k, v := range config.Environments[env].Dcs[dc].Vars {
				fmt.Printf("export %s=%q\n", k, v)
			}

			fmt.Printf("# Datacenter (%s) Specific Secrets:\n", env)
			for k, path := range config.Environments[env].Dcs[dc].Secrets {
				secret, err := GetVaultSecret(path)
				if err == nil {
					fmt.Printf("export %s=%q # %s\n", k, secret.Data["value"], path)
				} else {
					return cli.NewExitError(err.Error(), VaultErrorCode)
				}
			}
		}

		return nil
	}

	app.Run(os.Args)
}
