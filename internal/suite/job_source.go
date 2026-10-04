package suite

import "errors"

// DeclaredJobTest answers an exact declared job through the suite's existing
// expansion owner. It does not prepare a target or read mutable input files.
func (d Document) DeclaredJobTest(environment, job string) (string, error) {
	declarations, err := d.declare(environment)
	if err != nil {
		return "", err
	}
	for _, declared := range declarations {
		if declared.Job == job {
			return declared.Test, nil
		}
	}
	return "", errors.New("the job is not declared by this suite")
}
