package main

import (
	"io/ioutil"
	"log"
	"net/http"

	"github.com/kpango/gache"
	"github.com/kpango/glog"
)

func main() {
	http.Handle("/", glog.HTTPLogger("sample", func(w http.ResponseWriter, r *http.Request) {

		sc, ok := gache.SGet(r) // get server side cache

		if ok {
			w.WriteHeader(sc.Status)
			w.Write(sc.Body)
			return
		}

		req, err := http.NewRequest(http.MethodGet, "https://github.com/kpango", nil)

		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		cc, ok := gache.CGet(req)

		var res *http.Response

		if ok {
			res = cc.Res
		} else {
			res, err = http.DefaultClient.Do(req)
			if err != nil {
				log.Println(err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			gache.CSet(req, res)
		}

		b, err := ioutil.ReadAll(res.Body)

		if err != nil {
			log.Println(err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		go func() {
			err := gache.SSet(r, http.StatusOK, nil, b) // set server side cache

			if err != nil {
				log.Println(err)
			}
		}()

		w.WriteHeader(http.StatusOK)
		w.Write(b)
	}))

	http.ListenAndServe(":9090", nil)
}
