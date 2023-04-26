/*
Copyright 2023.

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

package webhook

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/go-logr/logr"

	admissionv1 "k8s.io/api/admission/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	ctrl "sigs.k8s.io/controller-runtime"
)

// StarburstEnterpriseValidator is in charge of starting up the web server and serving our router
type StarburstEnterpriseValidator struct {
	ctrl.Manager
	logr.Logger
	runtime.Decoder
}

// ErrorLoggerWrapper is used for relaying the server error logger messages to our own logger
type ErrorLoggerWrapper struct {
	logr.Logger
}

func (w *ErrorLoggerWrapper) Write(p []byte) (int, error) {
	w.Logger.Error(errors.New(string(p)), "")
	return len(p), nil
}

// our validator instance
var validator *StarburstEnterpriseValidator

// add our validator to a manager
func Add(m ctrl.Manager) error {
	validator = &StarburstEnterpriseValidator{
		m,
		m.GetLogger(),
		serializer.NewCodecFactory(m.GetScheme()).UniversalDecoder(),
	}
	return m.Add(validator)
}

// start up the web server once once invoked by the manager
func (v *StarburstEnterpriseValidator) Start(ctx context.Context) error {
	if Flags.TlsCert == "" || Flags.TlsKey == "" {
		err := fmt.Errorf("missing arguments, 'tls-cert' and 'tls-key' are required")
		v.Logger.Error(err, "can not start validator without a certificate")
		return err
	}

	if Flags.Port < 0 {
		err := fmt.Errorf("wrong argument, 'port' must be a positive value")
		v.Logger.Error(err, "can not start validator without a port")
		return err
	}

	// load certificate from key pair
	cert, err := tls.LoadX509KeyPair(Flags.TlsCert, Flags.TlsKey)
	if err != nil {
		err := fmt.Errorf("certificate issue, failed to load certificate and key")
		v.Logger.Error(err, "can not run without a certificate")
		return err
	}

	// create router
	router := http.NewServeMux()
	router.HandleFunc("/validate-enterprise", v.validateEnterprise)

	// create server
	webhookServer := &http.Server{
		Addr:    fmt.Sprintf(":%d", Flags.Port),
		Handler: router,
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
		},
		ErrorLog: log.New(&ErrorLoggerWrapper{v.Logger}, "https", log.LstdFlags),
	}

	// run server
	if err := webhookServer.ListenAndServeTLS("", ""); err != nil {
		v.Logger.Error(err, "failed to start webhook server")
		return err
	}

	return nil
}

// function for handling all validation requests
func (v *StarburstEnterpriseValidator) validateEnterprise(writer http.ResponseWriter, request *http.Request) {
	if err := verifyRequest(request); err != nil {
		v.writeError(writer, err, "request not verified", 400)
		return
	}

	admissionRequest, err := parseRequest(v.Decoder, request)
	if err != nil {
		v.writeError(writer, err, "failed parsing request", 400)
		return
	}

	operation := admissionRequest.Request.Operation

	// create admission response for setting the review status
	admissionResponse := &admissionv1.AdmissionResponse{}
	admissionResponse.UID = admissionRequest.Request.UID
	admissionResponse.Allowed = isUserAllowed(admissionRequest.Request.UserInfo)

	if !admissionResponse.Allowed {
		// result is only read when not allowed
		admissionResponse.Result = &v1.Status{
			Status:  "Failure",
			Message: fmt.Sprintf("%s not allowed", operation),
			Code:    403,
			Reason:  "Unauthorized Access",
		}
	}

	// create admission review for responding
	admissionReview := &admissionv1.AdmissionReview{
		Response: admissionResponse,
	}
	admissionReview.SetGroupVersionKind(admissionRequest.GroupVersionKind())

	// serialize the response
	resp, err := json.Marshal(admissionReview)
	if err != nil {
		v.writeError(writer, err, "failed creating response", 500)
		return
	}

	// stream back http response
	writer.Header().Set("Content-Type", "application/json")
	writer.Write(resp)
}

// write error message to log and stream back as an http response
func (v *StarburstEnterpriseValidator) writeError(writer http.ResponseWriter, err error, msg string, code int) {
	v.Logger.Error(err, msg)
	writer.WriteHeader(code)
	writer.Write([]byte(msg))
}

// verify the request is of type application/json and has a legit json body
func verifyRequest(request *http.Request) error {
	if request.Header.Get("Content-Type") != "application/json" {
		return fmt.Errorf("expected application/json content-type")
	}

	if request.Body == nil {
		return fmt.Errorf("no request body found")
	}

	// is this an overkill?
	// if _, err := json.Marshal(request.Body); err != nil {
	// 	return fmt.Errorf("failed parsing body")
	// }

	return nil
}

// use the decoder for parsing the http request into an admission review request
func parseRequest(decoder runtime.Decoder, request *http.Request) (*admissionv1.AdmissionReview, error) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		return nil, err
	}

	admissionReviewRequest := &admissionv1.AdmissionReview{}
	if _, _, err := decoder.Decode(body, nil, admissionReviewRequest); err != nil {
		return nil, err
	}

	return admissionReviewRequest, nil
}

// verify the requesting use is our own service account
func isUserAllowed(userInfo authenticationv1.UserInfo) bool {
	infoArray := strings.Split(userInfo.Username, ":")

	return infoArray[0] == "system" &&
		infoArray[1] == "serviceaccount" &&
		infoArray[3] == "starburst-addon-operator-controller-manager"
}
