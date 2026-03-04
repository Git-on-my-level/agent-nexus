package schema

func ReadVersion(path string) (string, error) {
	contract, err := Load(path)
	if err != nil {
		return "", err
	}
	return contract.Version, nil
}
