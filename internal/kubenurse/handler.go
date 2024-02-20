package kubenurse

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/postfinance/kubenurse/internal/kubediscovery"
	"github.com/postfinance/kubenurse/internal/servicecheck"
)

func (s *Server) readyHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()

		if s.ready {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	}
}

func (s *Server) aliveHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		type Output struct {
			Hostname   string              `json:"hostname"`
			Headers    map[string][]string `json:"headers"`
			UserAgent  string              `json:"user_agent"`
			RequestURI string              `json:"request_uri"`
			RemoteAddr string              `json:"remote_addr"`

			// checker.Result
			servicecheck.Result

			// kubediscovery
			NeighbourhoodState string                    `json:"neighbourhood_state"`
			Neighbourhood      []kubediscovery.Neighbour `json:"neighbourhood"`
		}

		// Run checks now
		res, haserr := s.checker.Run()
		if haserr {
			w.WriteHeader(http.StatusInternalServerError)
		}

		// Add additional data
		out := Output{
			Result:             res,
			Headers:            r.Header,
			UserAgent:          r.UserAgent(),
			RequestURI:         r.RequestURI,
			RemoteAddr:         r.RemoteAddr,
			Neighbourhood:      res.Neighbourhood,
			NeighbourhoodState: res.NeighbourhoodState,
		}
		out.Hostname, _ = os.Hostname()

		// Generate output output
		enc := json.NewEncoder(w)
		enc.SetIndent("", " ")
		_ = enc.Encode(out)
	}
}

func (s *Server) diagnoseHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" {
			body, err := ioutil.ReadAll(r.Body)
			if err != nil {
				// log.Printf("failed request for read body with %v", err)
				http.Error(w, "Error reading request body",
					http.StatusInternalServerError)
			}
			diagnose := &Diagnose{}
			err = json.Unmarshal(body, diagnose)
			if err != nil {
				// log.Printf("failed request for Unmarshal with %v", err)
				http.Error(w, "Error reading request body",
					http.StatusInternalServerError)
			}
			// Run checks now
			haserr := s.checker.RunFunc(diagnose.CheckType, diagnose.CheckProtocal, diagnose.CheckDstEndpoint, diagnose.CheckIngressInCluster)
			if haserr {
				// w.WriteHeader(http.StatusInternalServerError)
				// log.Printf("failed request for %s %s %swith %v", diagnose.CheckType, diagnose.CheckProtocal, diagnose.CheckDstEndpoint, err)
			}
		} else {
			http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		}
		// type Output struct {
		// 	Hostname   string              `json:"hostname"`
		// 	Headers    map[string][]string `json:"headers"`
		// 	UserAgent  string              `json:"user_agent"`
		// 	RequestURI string              `json:"request_uri"`
		// 	RemoteAddr string              `json:"remote_addr"`

		// 	// checker.Result
		// 	servicecheck.Result

		// 	// kubediscovery
		// 	NeighbourhoodState string                    `json:"neighbourhood_state"`
		// 	Neighbourhood      []kubediscovery.Neighbour `json:"neighbourhood"`
		// }

		// // Run checks now
		// res, haserr := s.checker.RunFunc()
		// if haserr {
		// 	w.WriteHeader(http.StatusInternalServerError)
		// }

		// // Add additional data
		// out := Output{
		// 	Result:             res,
		// 	Headers:            r.Header,
		// 	UserAgent:          r.UserAgent(),
		// 	RequestURI:         r.RequestURI,
		// 	RemoteAddr:         r.RemoteAddr,
		// 	Neighbourhood:      res.Neighbourhood,
		// 	NeighbourhoodState: res.NeighbourhoodState,
		// }
		// out.Hostname, _ = os.Hostname()

		// // Generate output output
		// enc := json.NewEncoder(w)
		// enc.SetIndent("", " ")
		// _ = enc.Encode(out)
	}
}
