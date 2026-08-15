// SPDX-License-Identifier: AGPL-3.0-only

package knowledgeadmission

import "gopkg.in/yaml.v3"

func yamlUnmarshal(data []byte, out any) error { return yaml.Unmarshal(data, out) }
