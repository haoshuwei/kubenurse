package servicecheck

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/postfinance/kubenurse/pkg/constant"
)

const (
	//nolint:gosec // This is the well-known path to Kubernetes serviceaccount tokens.
	K8sTokenFile = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	k8sCAFile    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

// doRequest does an http request only to get the http status code
func (c *Checker) doRequest(ctx context.Context, url string) (string, error) {
	// Read Bearer Token file from ServiceAccount
	token, err := os.ReadFile(K8sTokenFile)
	if err != nil {
		return errStr, fmt.Errorf("load kubernetes serviceaccount token from %s: %w", K8sTokenFile, err)
	}

	if !strings.HasPrefix(url, "http://") {
		url = fmt.Sprintf("http://%s", url)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)

	// Only add the Bearer for API Server Requests
	if strings.HasSuffix(url, "/version") {
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err.Error(), err
	}

	// Body is non-nil if err is nil, so close it
	_ = resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return okStr, nil
	}

	return resp.Status, errors.New(resp.Status)
}

// generateTLSConfig returns a TLSConfig including K8s CA and the user-defined extraCA
func generateTLSConfig(extraCA string) (*tls.Config, error) {
	// Append default certpool
	rootCAs, _ := x509.SystemCertPool()
	if rootCAs == nil {
		rootCAs = x509.NewCertPool()
	}

	// Append ServiceAccount cacert
	caCert, err := os.ReadFile(k8sCAFile)
	if err != nil {
		return nil, fmt.Errorf("could not load certificate %s: %w", k8sCAFile, err)
	}

	if ok := rootCAs.AppendCertsFromPEM(caCert); !ok {
		return nil, errors.New("could not append ca cert to system certpool")
	}

	// Append extra CA, if set
	if extraCA != "" {
		caCert, err := os.ReadFile(extraCA) // Intentionally included by the user.
		if err != nil {
			return nil, fmt.Errorf("could not load certificate %s: %w", extraCA, err)
		}

		if ok := rootCAs.AppendCertsFromPEM(caCert); !ok {
			return nil, errors.New("could not append extra ca cert to system certpool")
		}
	}

	// Configure transport
	tlsConfig := &tls.Config{
		RootCAs:    rootCAs,
		MinVersion: tls.VersionTLS12,
	}

	return tlsConfig, nil
}

// doRequest does an http request only to get the http status code
func (c *Checker) doHttpCheck(ctx context.Context, ep string) error {
	if !strings.HasPrefix(ep, "http://") {
		ep = fmt.Sprintf("http://%s", ep)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", ep, http.NoBody)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}

	// Body is non-nil if err is nil, so close it
	_ = resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	return errors.New(resp.Status)
}

func (c *Checker) doIngressCheck(ctx context.Context, ep string, inCluster bool) error {
	var host string
	var ingEp string
	if inCluster {
		reg := regexp.MustCompile(constant.Pattern)
		host = reg.FindString(ep)
		path := strings.TrimPrefix(ep, host)
		//ep = "http://10.10.10.117:32596"
		ingEp = fmt.Sprintf("http://%s%s", os.Getenv("UBINURSE_INGRESS_NODE_IP"), path)
		// log.Printf("+++ %s %s %s", host, path, ingEp)
	} else {
		ingEp = fmt.Sprintf("https://%s", ep)
	}

	req, _ := http.NewRequestWithContext(ctx, "GET", ingEp, http.NoBody)

	// Only add the Bearer for API Server Requests
	if inCluster {
		req.Host = host
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}

	// Body is non-nil if err is nil, so close it
	_ = resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	return errors.New(resp.Status)
}

func (c *Checker) doTcpCheck(ctx context.Context, ep string) error {
	conn, err := net.DialTimeout("tcp", ep, time.Second*3)
	if err != nil {
		return err
	} else {
		defer conn.Close()
	}
	return nil
}
