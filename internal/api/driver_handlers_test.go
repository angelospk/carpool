package api_test

import (
	"net/http"
	"testing"
)

func TestADriverRegistersTheirCarAndTheAppPricesIt(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000001", "Οδηγός")

	res := h.do(http.MethodPut, "/v1/me/driver", map[string]any{
		"make": "Toyota", "model": "Yaris", "fuel": "petrol_hybrid",
		"engine_cc": 1490, "plate": "ΑΒΓ-1234", "colour": "λευκό",
	}, token)
	if res.Code != http.StatusOK {
		t.Fatalf("PUT /v1/me/driver = %d: %s", res.Code, res.Body)
	}

	var got struct {
		Consumption float64 `json:"fuel_consumption_l_100km"`
		Source      string  `json:"consumption_source"`
		Verified    bool    `json:"verified"`
	}
	h.decode(res, &got)

	if got.Source != "dataset" || !got.Verified {
		t.Errorf("source %q verified %v, want a verified dataset match", got.Source, got.Verified)
	}
	if got.Consumption < 3 || got.Consumption > 6 {
		t.Errorf("a hybrid Yaris priced at %v l/100km", got.Consumption)
	}
}

// The contract, stated at the boundary. A client that tries to set the number
// has to be told no, not quietly ignored: silence is what lets somebody
// re-introduce the field later without noticing what it was for.
func TestADriverCannotSendTheirOwnConsumptionOrWearFactor(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000001", "Οδηγός")

	for _, smuggled := range []string{"fuel_consumption_l_100km", "wear_factor"} {
		body := map[string]any{
			"make": "Toyota", "model": "Yaris", "fuel": "petrol_hybrid",
			"engine_cc": 1490, "plate": "ΑΒΓ-1234",
			smuggled: 14.0,
		}
		res := h.do(http.MethodPut, "/v1/me/driver", body, token)
		if res.Code != http.StatusBadRequest {
			t.Errorf("PUT with %s = %d, want 400: %s", smuggled, res.Code, res.Body)
		}
	}
}

func TestTheVehiclePickerOffersMakesAndModels(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000001", "Οδηγός")

	res := h.do(http.MethodGet, "/v1/vehicles/makes", nil, token)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /v1/vehicles/makes = %d", res.Code)
	}
	var makes struct {
		Makes []string `json:"makes"`
	}
	h.decode(res, &makes)
	if len(makes.Makes) < 30 {
		t.Fatalf("%d makes offered", len(makes.Makes))
	}

	res = h.do(http.MethodGet, "/v1/vehicles/models?make=TOYOTA", nil, token)
	if res.Code != http.StatusOK {
		t.Fatalf("GET /v1/vehicles/models = %d", res.Code)
	}
	var models struct {
		Models []map[string]any `json:"models"`
	}
	h.decode(res, &models)
	if len(models.Models) == 0 {
		t.Fatal("no Toyota models offered")
	}
}

// A make with nothing in the dataset is an ordinary answer, because the app's
// next move is to ask for the CO2 figure, not to tell the driver their car
// does not exist.
func TestAnUnknownMakeReturnsAnEmptyListRatherThanAnError(t *testing.T) {
	h := newHarness(t)
	token, _ := h.signUp("+306900000001", "Οδηγός")

	res := h.do(http.MethodGet, "/v1/vehicles/models?make=Delorean", nil, token)
	if res.Code != http.StatusOK {
		t.Fatalf("GET = %d, want 200: %s", res.Code, res.Body)
	}
	var models struct {
		Models []map[string]any `json:"models"`
	}
	h.decode(res, &models)
	if len(models.Models) != 0 {
		t.Errorf("%d models for a make that is not in the data", len(models.Models))
	}
}
