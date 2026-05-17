package cmd

import (
	"errors"
	"io/ioutil"
	"net/http"

	consul "github.com/hashicorp/consul/api"
	"github.com/yyyar/gobetween/config"
	"github.com/yyyar/gobetween/utils"
	"github.com/yyyar/gobetween/utils/codec"
)

var configLoader func() (*config.Config, error)

func setConfigLoader(loader func() (*config.Config, error)) {
	configLoader = loader
}

func LoadConfig() (*config.Config, error) {
	if configLoader == nil {
		return nil, errors.New("configuration reload is not available for this command")
	}

	return configLoader()
}

func decodeConfigString(data string) (*config.Config, error) {
	if isConfigEnvVars {
		data = utils.SubstituteEnvVars(data)
	}

	var cfg config.Config
	if err := codec.Decode(data, &cfg, format); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func loadConfigFromFile(path string) (*config.Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}

	return decodeConfigString(string(data))
}

func loadConfigFromURL(url string) (*config.Config, error) {
	client := http.Client{}
	res, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	content, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}

	return decodeConfigString(string(content))
}

func loadConfigFromConsul(address, key string, cfg consul.Config) (*config.Config, error) {
	cfg.Address = address

	client, err := consul.NewClient(&cfg)
	if err != nil {
		return nil, err
	}

	pair, _, err := client.KV().Get(key, nil)
	if err != nil {
		return nil, err
	}

	if pair == nil {
		return nil, errors.New("empty value for key " + key)
	}

	return decodeConfigString(string(pair.Value))
}
