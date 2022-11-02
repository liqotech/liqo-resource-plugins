// Copyright 2019-2022 The Liqo Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package args

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/api/resource"

	liqoargs "github.com/liqotech/liqo/pkg/utils/args"
)

// QuantityMap implements the flag.Value interface and allows to parse strings expressing resource quantities.
type QuantityMap struct {
	StringValues liqoargs.StringList
	ResourceMap  map[string]*resource.Quantity
}

// String returns the stringified map entries.
func (q *QuantityMap) String() string {
	return q.StringValues.String()
}

// Set parses the provided string as a resource quantity and put it in the map.
func (q *QuantityMap) Set(str string) error {
	if q.ResourceMap == nil {
		q.ResourceMap = make(map[string]*resource.Quantity)
	}

	if err := q.StringValues.Set(str); err != nil {
		return err
	}

	for _, entry := range q.StringValues.StringList {
		key, quantity, err := parseQuantity(entry)
		if err != nil {
			return err
		}
		q.ResourceMap[key] = quantity
	}

	return nil
}

// Type return the type name.
func (q *QuantityMap) Type() string {
	return "quantityList"
}

func parseQuantity(str string) (string, *resource.Quantity, error) {
	res := strings.Split(str, "=")

	if len(res) != 2 {
		return "", nil, fmt.Errorf("invalid resource format %s", str)
	}

	if res[0] == "" || res[1] == "" {
		return "", nil, fmt.Errorf("invalid resource format %s", str)
	}

	quantity, err := resource.ParseQuantity(res[1])
	if err != nil {
		return "", nil, err
	}

	return res[0], &quantity, nil
}
