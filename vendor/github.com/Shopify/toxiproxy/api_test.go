package toxiproxy_test

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"testing"
	"time"

	"github.com/Shopify/toxiproxy"
	tclient "github.com/Shopify/toxiproxy/client"
)

var testServer *toxiproxy.ApiServer

var client = tclient.NewClient("http://127.0.0.1:8475")

func WithServer(t *testing.T, f func(string)) {
	// Make sure only one server is running at a time. Apparently there's no clean
	// way to shut it down between each test run.
	if testServer == nil {
		testServer = toxiproxy.NewServer()
		go testServer.Listen("localhost", "8475")

		// Allow server to start. There's no clean way to know when it listens.
		time.Sleep(50 * time.Millisecond)
	}

	defer func() {
		err := testServer.Collection.Clear()
		if err != nil {
			t.Error("Failed to clear collection", err)
		}
	}()

	f("http://localhost:8475")
}

func TestBrowserGets403(t *testing.T) {
	WithServer(t, func(addr string) {
		client := http.Client{}

		req, _ := http.NewRequest("GET", "http://localhost:8475/proxies", nil)
		req.Header.Add("User-Agent", "Mozilla/5.0 (Linux; Android 4.4.2); Nexus 5 Build/KOT49H) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/33.0.1750.117 Mobile Safari/537.36 OPR/20.0.1396.72047")

		resp, _ := client.Do(req)

		if resp.StatusCode != 403 {
			t.Fatal("Browser-like UserAgent was not denied access to Toxiproxy")
		}
	})
}

func TestNonBrowserGets200(t *testing.T) {
	WithServer(t, func(addr string) {
		client := http.Client{}

		req, _ := http.NewRequest("GET", "http://localhost:8475/proxies", nil)
		req.Header.Add("User-Agent", "Wget/2.1")

		resp, _ := client.Do(req)

		if resp.StatusCode == 403 {
			t.Fatal("Non-Browser-like UserAgent was denied access to Toxiproxy")
		}
	})
}

func TestIndexWithNoProxies(t *testing.T) {
	WithServer(t, func(addr string) {
		client := tclient.NewClient(addr)
		proxies, err := client.Proxies()
		if err != nil {
			t.Fatal("Failed getting proxies:", err)
		}

		if len(proxies) > 0 {
			t.Fatal("Expected no proxies, got:", proxies)
		}
	})
}

func TestCreateProxyBlankName(t *testing.T) {
	WithServer(t, func(addr string) {
		_, err := client.CreateProxy("", "", "")
		if err == nil {
			t.Fatal("Expected error creating proxy, got nil")
		} else if err.Error() != "Create: HTTP 400: missing required field: name" {
			t.Fatal("Expected different error creating proxy:", err)
		}
	})
}

func TestCreateProxyBlankUpstream(t *testing.T) {
	WithServer(t, func(addr string) {
		_, err := client.CreateProxy("test", "", "")
		if err == nil {
			t.Fatal("Expected error creating proxy, got nil")
		} else if err.Error() != "Create: HTTP 400: missing required field: upstream" {
			t.Fatal("Expected different error creating proxy:", err)
		}
	})
}

func TestPopulateProxy(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxies, err := client.Populate([]tclient.Proxy{
			{
				Name:     "one",
				Listen:   "localhost:7070",
				Upstream: "localhost:7171",
				Enabled:  true,
			},
			{
				Name:     "two",
				Listen:   "localhost:7373",
				Upstream: "localhost:7474",
				Enabled:  true,
			},
		})

		if err != nil {
			t.Fatal("Unable to populate:", err)
		} else if len(testProxies) != 2 {
			t.Fatalf("Wrong number of proxies returned: %d != 2", len(testProxies))
		} else if testProxies[0].Name != "one" || testProxies[1].Name != "two" {
			t.Fatalf("Wrong proxy names returned: %s, %s", testProxies[0].Name, testProxies[1].Name)
		}

		for _, p := range testProxies {
			AssertProxyUp(t, p.Listen, true)
		}
	})
}

func TestPopulateDefaultEnabled(t *testing.T) {
	WithServer(t, func(addr string) {
		request := []byte(`[{"name": "test", "listen": "localhost:7070", "upstream": "localhost:7171"}]`)

		resp, err := http.Post(addr+"/populate", "application/json", bytes.NewReader(request))
		if err != nil {
			t.Fatal("Failed to send POST to /populate:", err)
		}

		if resp.StatusCode != http.StatusCreated {
			message, _ := ioutil.ReadAll(resp.Body)
			t.Fatalf("Failed to populate proxy list: HTTP %s\n%s", resp.Status, string(message))
		}

		proxies, err := client.Proxies()
		if err != nil {
			t.Fatal(err)
		} else if len(proxies) != 1 {
			t.Fatalf("Wrong number of proxies created: %d != 1", len(proxies))
		} else if _, ok := proxies["test"]; !ok {
			t.Fatalf("Wrong proxy name returned")
		}

		for _, p := range proxies {
			AssertProxyUp(t, p.Listen, true)
		}
	})
}

func TestPopulateDisabledProxy(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxies, err := client.Populate([]tclient.Proxy{
			{
				Name:     "one",
				Listen:   "localhost:7070",
				Upstream: "localhost:7171",
				Enabled:  false,
			},
			{
				Name:     "two",
				Listen:   "localhost:7373",
				Upstream: "localhost:7474",
				Enabled:  true,
			},
		})

		if err != nil {
			t.Fatal("Unable to populate:", err)
		} else if len(testProxies) != 2 {
			t.Fatalf("Wrong number of proxies returned: %d != 2", len(testProxies))
		} else if testProxies[0].Name != "one" || testProxies[1].Name != "two" {
			t.Fatalf("Wrong proxy names returned: %s, %s", testProxies[0].Name, testProxies[1].Name)
		}

		AssertProxyUp(t, "localhost:7070", false)
		AssertProxyUp(t, "localhost:7373", true)
	})
}

func TestPopulateExistingProxy(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("one", "localhost:7070", "localhost:7171")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}
		_, err = client.CreateProxy("two", "localhost:7373", "localhost:7474")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		// Create a toxic so we can make sure the proxy wasn't replaced
		_, err = testProxy.AddToxic("", "latency", "downstream", 1, nil)
		if err != nil {
			t.Fatal("Unable to create toxic:", err)
		}

		testProxies, err := client.Populate([]tclient.Proxy{
			{
				Name:     "one",
				Listen:   "127.0.0.1:7070", // TODO(xthexder): Will replace existing proxy if not resolved ip...
				Upstream: "localhost:7171",
				Enabled:  true,
			},
			{
				Name:     "two",
				Listen:   "127.0.0.1:7575",
				Upstream: "localhost:7676",
				Enabled:  true,
			},
		})

		if err != nil {
			t.Fatal("Unable to populate:", err)
		} else if len(testProxies) != 2 {
			t.Fatalf("Wrong number of proxies returned: %d != 2", len(testProxies))
		} else if testProxies[0].Name != "one" || testProxies[1].Name != "two" {
			t.Fatalf("Wrong proxy names returned: %s, %s", testProxies[0].Name, testProxies[1].Name)
		} else if testProxies[0].Listen != "127.0.0.1:7070" || testProxies[1].Listen != "127.0.0.1:7575" {
			t.Fatalf("Wrong proxy listen addresses returned: %s, %s", testProxies[0].Listen, testProxies[1].Listen)
		}

		toxics, err := testProxy.Toxics()
		if err != nil {
			t.Fatal("Unable to get toxics:", err)
		}
		if len(toxics) != 1 || toxics[0].Type != "latency" {
			t.Fatalf("Populate did not preseve existing proxy. (%d toxics)", len(toxics))
		}

		for _, p := range testProxies {
			AssertProxyUp(t, p.Listen, true)
		}
	})
}

func TestPopulateWithBadName(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxies, err := client.Populate([]tclient.Proxy{
			{
				Name:     "one",
				Listen:   "localhost:7070",
				Upstream: "localhost:7171",
				Enabled:  true,
			},
			{
				Name:    "",
				Listen:  "",
				Enabled: true,
			},
		})

		if err == nil {
			t.Fatal("Expected Populate to fail.")
		} else if err.Error() != "Populate: HTTP 400: missing required field: name at proxy 2" {
			t.Fatal("Expected different error during populate:", err)
		} else if len(testProxies) != 0 {
			t.Fatalf("Wrong number of proxies returned: %d != 0", len(testProxies))
		}

		proxies, err := client.Proxies()
		if err != nil {
			t.Fatal(err)
		} else if len(proxies) != 0 {
			t.Fatalf("Expected no proxies to be created: %d != 0", len(proxies))
		}
	})
}

func TestPopulateProxyWithBadDataShouldReturnError(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxies, err := client.Populate([]tclient.Proxy{
			{
				Name:     "one",
				Listen:   "localhost:7070",
				Upstream: "localhost:7171",
				Enabled:  true,
			},
			{
				Name:     "two",
				Listen:   "local373",
				Upstream: "localhost:7474",
				Enabled:  true,
			},
			{
				Name:     "three",
				Listen:   "localhost:7575",
				Upstream: "localhost:7676",
				Enabled:  true,
			},
		})

		if err == nil {
			t.Fatal("Expected Populate to fail.")
		} else if len(testProxies) != 1 {
			t.Fatalf("Wrong number of proxies returned: %d != %d", len(testProxies), 1)
		} else if testProxies[0].Name != "one" {
			t.Fatalf("Wrong proxy name returned: %s != one", testProxies[0].Name)
		}

		for _, p := range testProxies {
			AssertProxyUp(t, p.Listen, true)
		}

		proxies, err := client.Proxies()
		if err != nil {
			t.Fatal(err)
		}

		for _, p := range proxies {
			if p.Name == "two" || p.Name == "three" {
				t.Fatalf("Proxy %s exists, populate did not fail correctly.")
			}
		}
	})
}

func TestListingProxies(t *testing.T) {
	WithServer(t, func(addr string) {
		_, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		proxies, err := client.Proxies()
		if err != nil {
			t.Fatal("Error listing proxies:", err)
		}

		if len(proxies) == 0 {
			t.Fatal("Expected new proxy in list")
		}
		proxy, ok := proxies["mysql_master"]
		if !ok {
			t.Fatal("Expected to see mysql_master proxy in list")
		}
		if proxy.Name != "mysql_master" || proxy.Listen != "127.0.0.1:3310" || proxy.Upstream != "localhost:20001" {
			t.Fatalf("Unexpected proxy metadata: %s, %s, %s", proxy.Name, proxy.Listen, proxy.Upstream)
		}
		AssertToxicExists(t, proxy.ActiveToxics, "latency", "", "", false)
	})
}

func TestCreateAndGetProxy(t *testing.T) {
	WithServer(t, func(addr string) {
		_, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		proxy, err := client.Proxy("mysql_master")
		if err != nil {
			t.Fatal("Unable to retriecve proxy:", err)
		}

		if proxy.Name != "mysql_master" || proxy.Listen != "127.0.0.1:3310" || proxy.Upstream != "localhost:20001" || !proxy.Enabled {
			t.Fatalf("Unexpected proxy metadata: %s, %s, %s, %v", proxy.Name, proxy.Listen, proxy.Upstream, proxy.Enabled)
		}

		AssertToxicExists(t, proxy.ActiveToxics, "latency", "", "", false)
	})
}

func TestCreateProxyWithSave(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy := client.NewProxy()
		testProxy.Name = "mysql_master"
		testProxy.Listen = "localhost:3310"
		testProxy.Upstream = "localhost:20001"
		testProxy.Enabled = true

		err := testProxy.Save()
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		proxy, err := client.Proxy("mysql_master")
		if err != nil {
			t.Fatal("Unable to retriecve proxy:", err)
		}

		if proxy.Name != "mysql_master" || proxy.Listen != "127.0.0.1:3310" || proxy.Upstream != "localhost:20001" || !proxy.Enabled {
			t.Fatalf("Unexpected proxy metadata: %s, %s, %s, %v", proxy.Name, proxy.Listen, proxy.Upstream, proxy.Enabled)
		}

		AssertProxyUp(t, proxy.Listen, true)
	})
}

func TestCreateDisabledProxy(t *testing.T) {
	WithServer(t, func(addr string) {
		disabledProxy := client.NewProxy()
		disabledProxy.Name = "mysql_master"
		disabledProxy.Listen = "localhost:3310"
		disabledProxy.Upstream = "localhost:20001"

		err := disabledProxy.Save()
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		proxy, err := client.Proxy("mysql_master")
		if err != nil {
			t.Fatal("Unable to retriecve proxy:", err)
		}

		if proxy.Name != "mysql_master" || proxy.Listen != "localhost:3310" || proxy.Upstream != "localhost:20001" || proxy.Enabled {
			t.Fatalf("Unexpected proxy metadata: %s, %s, %s, %v", proxy.Name, proxy.Listen, proxy.Upstream, proxy.Enabled)
		}

		AssertProxyUp(t, proxy.Listen, false)
	})
}

func TestCreateDisabledProxyAndEnable(t *testing.T) {
	WithServer(t, func(addr string) {
		disabledProxy := client.NewProxy()
		disabledProxy.Name = "mysql_master"
		disabledProxy.Listen = "localhost:3310"
		disabledProxy.Upstream = "localhost:20001"

		err := disabledProxy.Save()
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		proxy, err := client.Proxy("mysql_master")
		if err != nil {
			t.Fatal("Unable to retriecve proxy:", err)
		}

		if proxy.Name != "mysql_master" || proxy.Listen != "localhost:3310" || proxy.Upstream != "localhost:20001" || proxy.Enabled {
			t.Fatalf("Unexpected proxy metadata: %s, %s, %s, %v", proxy.Name, proxy.Listen, proxy.Upstream, proxy.Enabled)
		}

		proxy.Enabled = true

		err = proxy.Save()
		if err != nil {
			t.Fatal("Failed to update proxy:", err)
		}

		AssertProxyUp(t, proxy.Listen, true)

		proxy.Enabled = false

		err = proxy.Save()
		if err != nil {
			t.Fatal("Failed to update proxy:", err)
		}

		AssertProxyUp(t, proxy.Listen, false)
	})
}

func TestDeleteProxy(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		proxies, err := client.Proxies()
		if err != nil {
			t.Fatal("Error listing proxies:", err)
		}

		if len(proxies) == 0 {
			t.Fatal("Expected new proxy in list")
		}

		AssertProxyUp(t, testProxy.Listen, true)

		err = testProxy.Delete()
		if err != nil {
			t.Fatal("Failed deleting proxy:", err)
		}

		AssertProxyUp(t, testProxy.Listen, false)

		proxies, err = client.Proxies()
		if err != nil {
			t.Fatal("Error listing proxies:", err)
		}

		if len(proxies) > 0 {
			t.Fatal("Expected proxy to be deleted from list")
		}

		err = testProxy.Delete()
		if err == nil {
			t.Fatal("Proxy did not result in not found.")
		} else if err.Error() != "Delete: HTTP 404: proxy not found" {
			t.Fatal("Incorrect error removing proxy:", err)
		}
	})
}

func TestCreateProxyPortConflict(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		_, err = client.CreateProxy("test", "localhost:3310", "localhost:20001")
		if err == nil {
			t.Fatal("Proxy did not result in conflict.")
		} else if err.Error() != "Create: HTTP 500: listen tcp 127.0.0.1:3310: bind: address already in use" {
			t.Fatal("Incorrect error adding proxy:", err)
		}

		err = testProxy.Delete()
		if err != nil {
			t.Fatal("Unable to delete proxy:", err)
		}
		_, err = client.CreateProxy("test", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}
	})
}

func TestCreateProxyNameConflict(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		_, err = client.CreateProxy("mysql_master", "localhost:3311", "localhost:20001")
		if err == nil {
			t.Fatal("Proxy did not result in conflict.")
		} else if err.Error() != "Create: HTTP 409: proxy already exists" {
			t.Fatal("Incorrect error adding proxy:", err)
		}

		err = testProxy.Delete()
		if err != nil {
			t.Fatal("Unable to delete proxy:", err)
		}
		_, err = client.CreateProxy("mysql_master", "localhost:3311", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}
	})
}

func TestResetState(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		latency, err := testProxy.AddToxic("", "latency", "downstream", 1, tclient.Attributes{
			"latency": 100,
			"jitter":  10,
		})
		if err != nil {
			t.Fatal("Error setting toxic:", err)
		}

		if latency.Attributes["latency"] != 100.0 || latency.Attributes["jitter"] != 10.0 {
			t.Fatal("Latency toxic did not start up with correct settings")
		}

		err = client.ResetState()
		if err != nil {
			t.Fatal("unable to reset state:", err)
		}

		proxies, err := client.Proxies()
		if err != nil {
			t.Fatal("Error listing proxies:", err)
		}

		proxy, ok := proxies["mysql_master"]
		if !ok {
			t.Fatal("Expected proxy to still exist")
		}
		if !proxy.Enabled {
			t.Fatal("Expected proxy to be enabled")
		}

		toxics, err := proxy.Toxics()
		if err != nil {
			t.Fatal("Error requesting toxics:", err)
		}

		AssertToxicExists(t, toxics, "latency", "", "", false)

		AssertProxyUp(t, proxy.Listen, true)
	})
}

func TestListingToxics(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		toxics, err := testProxy.Toxics()
		if err != nil {
			t.Fatal("Error returning toxics:", err)
		}

		AssertToxicExists(t, toxics, "latency", "", "", false)
	})
}

func TestAddToxic(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		latency, err := testProxy.AddToxic("foobar", "latency", "downstream", 1, tclient.Attributes{
			"latency": 100,
			"jitter":  10,
		})
		if err != nil {
			t.Fatal("Error setting toxic:", err)
		}

		if latency.Attributes["latency"] != 100.0 || latency.Attributes["jitter"] != 10.0 {
			t.Fatal("Latency toxic did not start up with correct settings")
		}

		toxics, err := testProxy.Toxics()
		if err != nil {
			t.Fatal("Error returning toxics:", err)
		}
		toxic := AssertToxicExists(t, toxics, "foobar", "latency", "downstream", true)
		if toxic.Toxicity != 1.0 || toxic.Attributes["latency"] != 100.0 || toxic.Attributes["jitter"] != 10.0 {
			t.Fatal("Toxic was not read back correctly:", toxic)
		}
	})
}

func TestAddMultipleToxics(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		_, err = testProxy.AddToxic("latency1", "latency", "downstream", 1, nil)
		if err != nil {
			t.Fatal("Error setting toxic:", err)
		}

		_, err = testProxy.AddToxic("latency2", "latency", "downstream", 1, nil)
		if err != nil {
			t.Fatal("Error setting toxic:", err)
		}

		toxics, err := testProxy.Toxics()
		if err != nil {
			t.Fatal("Error returning toxics:", err)
		}
		AssertToxicExists(t, toxics, "latency1", "latency", "downstream", true)
		toxic := AssertToxicExists(t, toxics, "latency2", "latency", "downstream", true)
		if toxic.Toxicity != 1.0 || toxic.Attributes["latency"] != 0.0 || toxic.Attributes["jitter"] != 0.0 {
			t.Fatal("Toxic was not read back correctly:", toxic)
		}
		AssertToxicExists(t, toxics, "latency1", "", "upstream", false)
		AssertToxicExists(t, toxics, "latency2", "", "upstream", false)
	})
}

func TestAddConflictingToxic(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		_, err = testProxy.AddToxic("foobar", "latency", "downstream", 1, nil)
		if err != nil {
			t.Fatal("Error setting toxic:", err)
		}

		_, err = testProxy.AddToxic("foobar", "slow_close", "downstream", 1, nil)
		if err == nil {
			t.Fatal("Toxic did not result in conflict.")
		} else if err.Error() != "AddToxic: HTTP 409: toxic already exists" {
			t.Fatal("Incorrect error setting toxic:", err)
		}

		toxics, err := testProxy.Toxics()
		if err != nil {
			t.Fatal("Error returning toxics:", err)
		}
		toxic := AssertToxicExists(t, toxics, "foobar", "latency", "downstream", true)
		if toxic.Toxicity != 1.0 || toxic.Attributes["latency"] != 0.0 || toxic.Attributes["jitter"] != 0.0 {
			t.Fatal("Toxic was not read back correctly:", toxic)
		}
		AssertToxicExists(t, toxics, "foobar", "", "upstream", false)
	})
}

func TestAddConflictingToxicsMultistream(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		_, err = testProxy.AddToxic("foobar", "latency", "upstream", 1, nil)
		if err != nil {
			t.Fatal("Error setting toxic:", err)
		}

		_, err = testProxy.AddToxic("foobar", "latency", "downstream", 1, nil)
		if err == nil {
			t.Fatal("Toxic did not result in conflict.")
		} else if err.Error() != "AddToxic: HTTP 409: toxic already exists" {
			t.Fatal("Incorrect error setting toxic:", err)
		}

		toxics, err := testProxy.Toxics()
		if err != nil {
			t.Fatal("Error returning toxics:", err)
		}

		toxic := AssertToxicExists(t, toxics, "foobar", "latency", "upstream", true)
		if toxic.Toxicity != 1.0 || toxic.Attributes["latency"] != 0.0 || toxic.Attributes["jitter"] != 0.0 {
			t.Fatal("Toxic was not read back correctly:", toxic)
		}
		AssertToxicExists(t, toxics, "foobar", "", "downstream", false)
	})
}

func TestAddConflictingToxicsMultistreamDefaults(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		_, err = testProxy.AddToxic("", "latency", "upstream", 1, nil)
		if err != nil {
			t.Fatal("Error setting toxic:", err)
		}

		_, err = testProxy.AddToxic("", "latency", "downstream", 1, nil)
		if err != nil {
			t.Fatal("Error setting toxic:", err)
		}

		toxics, err := testProxy.Toxics()
		if err != nil {
			t.Fatal("Error returning toxics:", err)
		}
		toxic := AssertToxicExists(t, toxics, "latency_upstream", "latency", "upstream", true)
		if toxic.Toxicity != 1.0 || toxic.Attributes["latency"] != 0.0 || toxic.Attributes["jitter"] != 0.0 {
			t.Fatal("Toxic was not read back correctly:", toxic)
		}
		toxic = AssertToxicExists(t, toxics, "latency_downstream", "latency", "downstream", true)
		if toxic.Toxicity != 1.0 || toxic.Attributes["latency"] != 0.0 || toxic.Attributes["jitter"] != 0.0 {
			t.Fatal("Toxic was not read back correctly:", toxic)
		}
	})
}

func TestAddToxicWithToxicity(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		latency, err := testProxy.AddToxic("", "latency", "downstream", 0.2, tclient.Attributes{
			"latency": 100,
			"jitter":  10,
		})
		if err != nil {
			t.Fatal("Error setting toxic:", err)
		}

		if latency.Toxicity != 0.2 || latency.Attributes["latency"] != 100.0 || latency.Attributes["jitter"] != 10.0 {
			t.Fatal("Latency toxic did not start up with correct settings:", latency)
		}

		toxics, err := testProxy.Toxics()
		if err != nil {
			t.Fatal("Error returning toxics:", err)
		}
		toxic := AssertToxicExists(t, toxics, "latency_downstream", "latency", "downstream", true)
		if toxic.Toxicity != 0.2 || toxic.Attributes["latency"] != 100.0 || toxic.Attributes["jitter"] != 10.0 {
			t.Fatal("Toxic was not read back correctly:", toxic)
		}
	})
}

func TestAddNoop(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		noop, err := testProxy.AddToxic("foobar", "noop", "", 1, nil)
		if err != nil {
			t.Fatal("Error setting toxic:", err)
		}

		if noop.Toxicity != 1.0 || noop.Name != "foobar" || noop.Type != "noop" || noop.Stream != "downstream" {
			t.Fatal("Noop toxic did not start up with correct settings:", noop)
		}

		toxics, err := testProxy.Toxics()
		if err != nil {
			t.Fatal("Error returning toxics:", err)
		}
		toxic := AssertToxicExists(t, toxics, "foobar", "noop", "downstream", true)
		if toxic.Toxicity != 1.0 {
			t.Fatal("Toxic was not read back correctly:", toxic)
		}
	})
}

func TestUpdateToxics(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		latency, err := testProxy.AddToxic("", "latency", "downstream", -1, tclient.Attributes{
			"latency": 100,
			"jitter":  10,
		})
		if err != nil {
			t.Fatal("Error setting toxic:", err)
		}

		if latency.Toxicity != 1.0 || latency.Attributes["latency"] != 100.0 || latency.Attributes["jitter"] != 10.0 {
			t.Fatal("Latency toxic did not start up with correct settings:", latency)
		}

		latency, err = testProxy.UpdateToxic("latency_downstream", 0.5, tclient.Attributes{
			"latency": 1000,
		})
		if err != nil {
			t.Fatal("Error setting toxic:", err)
		}

		if latency.Toxicity != 0.5 || latency.Attributes["latency"] != 1000.0 || latency.Attributes["jitter"] != 10.0 {
			t.Fatal("Latency toxic did not get updated with the correct settings:", latency)
		}

		latency, err = testProxy.UpdateToxic("latency_downstream", -1, tclient.Attributes{
			"latency": 500,
		})
		if err != nil {
			t.Fatal("Error setting toxic:", err)
		}

		if latency.Toxicity != 0.5 || latency.Attributes["latency"] != 500.0 || latency.Attributes["jitter"] != 10.0 {
			t.Fatal("Latency toxic did not get updated with the correct settings:", latency)
		}

		toxics, err := testProxy.Toxics()
		if err != nil {
			t.Fatal("Error returning toxics:", err)
		}

		toxic := AssertToxicExists(t, toxics, "latency_downstream", "latency", "downstream", true)
		if toxic.Toxicity != 0.5 || toxic.Attributes["latency"] != 500.0 || toxic.Attributes["jitter"] != 10.0 {
			t.Fatal("Toxic was not read back correctly:", toxic)
		}
	})
}

func TestRemoveToxic(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		_, err = testProxy.AddToxic("", "latency", "downstream", 1, nil)
		if err != nil {
			t.Fatal("Error setting toxic:", err)
		}

		toxics, err := testProxy.Toxics()
		if err != nil {
			t.Fatal("Error returning toxics:", err)
		}

		toxic := AssertToxicExists(t, toxics, "latency_downstream", "latency", "downstream", true)
		if toxic.Toxicity != 1.0 || toxic.Attributes["latency"] != 0.0 || toxic.Attributes["jitter"] != 0.0 {
			t.Fatal("Toxic was not read back correctly:", toxic)
		}

		err = testProxy.RemoveToxic("latency_downstream")
		if err != nil {
			t.Fatal("Error removing toxic:", err)
		}

		toxics, err = testProxy.Toxics()
		if err != nil {
			t.Fatal("Error returning toxics:", err)
		}
		AssertToxicExists(t, toxics, "latency_downstream", "", "", false)
	})
}

func TestVersionEndpointReturnsVersion(t *testing.T) {
	WithServer(t, func(addr string) {
		resp, err := http.Get(addr + "/version")
		if err != nil {
			t.Fatal("Failed to get index", err)
		}

		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			t.Fatal("Unable to read body from response")
		}

		if string(body) != toxiproxy.Version {
			t.Fatal("Expected to return Version from /version, got:", string(body))
		}
	})
}

func TestInvalidStream(t *testing.T) {
	WithServer(t, func(addr string) {
		testProxy, err := client.CreateProxy("mysql_master", "localhost:3310", "localhost:20001")
		if err != nil {
			t.Fatal("Unable to create proxy:", err)
		}

		_, err = testProxy.AddToxic("", "latency", "walrustream", 1, nil)
		if err == nil {
			t.Fatal("Error setting toxic:", err)
		}
	})
}

func AssertToxicExists(t *testing.T, toxics tclient.Toxics, name, typeName, stream string, exists bool) *tclient.Toxic {
	var toxic *tclient.Toxic
	var actualType, actualStream string

	for _, tox := range toxics {
		if name == tox.Name {
			toxic = &tox
			actualType = tox.Type
			actualStream = tox.Stream
		}
	}
	if exists {
		if toxic == nil {
			t.Fatalf("Expected to see %s toxic in list", name)
		} else if actualType != typeName {
			t.Fatalf("Expected %s to be of type %s, found %s", name, typeName, actualType)
		} else if actualStream != stream {
			t.Fatalf("Expected %s to be in stream %s, found %s", name, stream, actualStream)
		}
	} else if toxic != nil && actualStream == stream {
		t.Fatalf("Expected %s toxic to be missing from list, found type %s", name, actualType)
	}
	return toxic
}
