/*
Copyright 2014 The Kubernetes Authors All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package cmd

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"testing"

	"github.com/spf13/cobra"
	"k8s.io/kubernetes/pkg/api"
	"k8s.io/kubernetes/pkg/api/testapi"
	client "k8s.io/kubernetes/pkg/client/unversioned"
	"k8s.io/kubernetes/pkg/client/unversioned/fake"
	cmdutil "k8s.io/kubernetes/pkg/kubectl/cmd/util"
	"k8s.io/kubernetes/pkg/util"
)

func TestGetRestartPolicy(t *testing.T) {
	tests := []struct {
		input       string
		interactive bool
		expected    api.RestartPolicy
		expectErr   bool
	}{
		{
			input:    "",
			expected: api.RestartPolicyAlways,
		},
		{
			input:       "",
			interactive: true,
			expected:    api.RestartPolicyOnFailure,
		},
		{
			input:       string(api.RestartPolicyAlways),
			interactive: true,
			expected:    api.RestartPolicyAlways,
		},
		{
			input:       string(api.RestartPolicyNever),
			interactive: true,
			expected:    api.RestartPolicyNever,
		},
		{
			input:    string(api.RestartPolicyAlways),
			expected: api.RestartPolicyAlways,
		},
		{
			input:    string(api.RestartPolicyNever),
			expected: api.RestartPolicyNever,
		},
		{
			input:     "foo",
			expectErr: true,
		},
	}
	for _, test := range tests {
		cmd := &cobra.Command{}
		cmd.Flags().String("restart", "", "dummy restart flag")
		cmd.Flags().Lookup("restart").Value.Set(test.input)
		policy, err := getRestartPolicy(cmd, test.interactive)
		if test.expectErr && err == nil {
			t.Error("unexpected non-error")
		}
		if !test.expectErr && err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if !test.expectErr && policy != test.expected {
			t.Errorf("expected: %s, saw: %s (%s:%v)", test.expected, policy, test.input, test.interactive)
		}
	}
}

func TestGetEnv(t *testing.T) {
	test := struct {
		input    []string
		expected []string
	}{
		input:    []string{"a=b", "c=d"},
		expected: []string{"a=b", "c=d"},
	}
	cmd := &cobra.Command{}
	cmd.Flags().StringSlice("env", test.input, "")

	envStrings := cmdutil.GetFlagStringSlice(cmd, "env")
	if len(envStrings) != 2 || !reflect.DeepEqual(envStrings, test.expected) {
		t.Errorf("expected: %s, saw: %s", test.expected, envStrings)
	}
}

func TestGenerateService(t *testing.T) {

	tests := []struct {
		port             string
		args             []string
		serviceGenerator string
		params           map[string]interface{}
		expectErr        bool
		name             string
		service          api.Service
		expectPOST       bool
	}{
		{
			port:             "80",
			args:             []string{"foo"},
			serviceGenerator: "service/v2",
			params: map[string]interface{}{
				"name": "foo",
			},
			expectErr: false,
			name:      "basic",
			service: api.Service{
				ObjectMeta: api.ObjectMeta{
					Name: "foo",
				},
				Spec: api.ServiceSpec{
					Ports: []api.ServicePort{
						{
							Port:       80,
							Protocol:   "TCP",
							TargetPort: util.NewIntOrStringFromInt(80),
						},
					},
					Selector: map[string]string{
						"run": "foo",
					},
					Type:            api.ServiceTypeClusterIP,
					SessionAffinity: api.ServiceAffinityNone,
				},
			},
			expectPOST: true,
		},
		{
			port:             "80",
			args:             []string{"foo"},
			serviceGenerator: "service/v2",
			params: map[string]interface{}{
				"name":   "foo",
				"labels": "app=bar",
			},
			expectErr: false,
			name:      "custom labels",
			service: api.Service{
				ObjectMeta: api.ObjectMeta{
					Name:   "foo",
					Labels: map[string]string{"app": "bar"},
				},
				Spec: api.ServiceSpec{
					Ports: []api.ServicePort{
						{
							Port:       80,
							Protocol:   "TCP",
							TargetPort: util.NewIntOrStringFromInt(80),
						},
					},
					Selector: map[string]string{
						"app": "bar",
					},
					Type:            api.ServiceTypeClusterIP,
					SessionAffinity: api.ServiceAffinityNone,
				},
			},
			expectPOST: true,
		},
		{
			expectErr:  true,
			name:       "missing port",
			expectPOST: false,
		},
		{
			port:             "80",
			args:             []string{"foo"},
			serviceGenerator: "service/v2",
			params: map[string]interface{}{
				"name": "foo",
			},
			expectErr:  false,
			name:       "dry-run",
			expectPOST: false,
		},
	}
	for _, test := range tests {
		sawPOST := false
		f, tf, codec := NewAPIFactory()
		tf.ClientConfig = &client.Config{Version: testapi.Default.Version()}
		tf.Client = &fake.RESTClient{
			Codec: codec,
			Client: fake.CreateHTTPClient(func(req *http.Request) (*http.Response, error) {
				switch p, m := req.URL.Path, req.Method; {
				case test.expectPOST && m == "POST" && p == "/namespaces/namespace/services":
					sawPOST = true
					body := objBody(codec, &test.service)
					data, err := ioutil.ReadAll(req.Body)
					if err != nil {
						t.Errorf("unexpected error: %v", err)
						t.FailNow()
					}
					defer req.Body.Close()
					svc := &api.Service{}
					if err := codec.DecodeInto(data, svc); err != nil {
						t.Errorf("unexpected error: %v", err)
						t.FailNow()
					}
					// Copy things that are defaulted by the system
					test.service.Annotations = svc.Annotations

					if !reflect.DeepEqual(&test.service, svc) {
						t.Errorf("expected:\n%v\nsaw:\n%v\n", &test.service, svc)
					}
					return &http.Response{StatusCode: 200, Body: body}, nil
				default:
					// Ensures no GET is performed when deleting by name
					t.Errorf("%s: unexpected request: %s %#v\n%#v", test.name, req.Method, req.URL, req)
					return nil, fmt.Errorf("unexpected request")
				}
			}),
		}
		cmd := &cobra.Command{}
		cmd.Flags().String("output", "", "")
		cmd.Flags().Bool(cmdutil.ApplyAnnotationsFlag, false, "")
		addRunFlags(cmd)

		if !test.expectPOST {
			cmd.Flags().Set("dry-run", "true")
		}

		if len(test.port) > 0 {
			cmd.Flags().Set("port", test.port)
			test.params["port"] = test.port
		}

		buff := &bytes.Buffer{}
		err := generateService(f, cmd, test.args, test.serviceGenerator, test.params, "namespace", buff)
		if test.expectErr {
			if err == nil {
				t.Error("unexpected non-error")
			}
			continue
		}
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if test.expectPOST != sawPOST {
			t.Error("expectPost: %v, sawPost: %v", test.expectPOST, sawPOST)
		}
	}
}
