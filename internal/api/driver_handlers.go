package api

import (
	"errors"
	"net/http"

	"carpool/internal/drivers"
	"carpool/internal/vehicles"
)

// handlePutDriver records the caller's car, and with it what the app decides
// the car drinks.
//
// There is deliberately no field for the consumption itself, and decodeJSON
// rejects unknown fields, so a client that sends one gets a 400 rather than
// being quietly ignored. That is the difference between a number the driver
// cannot set and a number the driver's edits are silently discarded from: the
// first is a contract, the second is a bug waiting to be re-introduced by
// somebody adding a field.
func (d Deps) handlePutDriver(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	var body struct {
		Make  string `json:"make"`
		Model string `json:"model"`
		Fuel  string `json:"fuel"`
		// EngineCC is off the registration document, not rounded by the client.
		EngineCC int `json:"engine_cc"`
		// CO2GramsPerKm is field V.7. Absent for cars first registered before
		// 1 November 2010, whose documents do not carry it.
		CO2GramsPerKm int    `json:"co2_g_km"`
		Colour        string `json:"colour"`
		Plate         string `json:"plate"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	profile, err := d.Drivers.Upsert(r.Context(), userID, drivers.Car{
		Make: body.Make, Model: body.Model, Fuel: body.Fuel,
		EngineCC: body.EngineCC, CO2GramsPerKm: body.CO2GramsPerKm,
		Colour: body.Colour, Plate: body.Plate,
	})
	if errors.Is(err, drivers.ErrInvalid) {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "the profile could not be saved")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"make": profile.Make, "model": profile.Model, "fuel": profile.Fuel,
		"engine_cc": profile.EngineCC, "co2_g_km": profile.CO2GramsPerKm,
		"colour": profile.Colour, "plate": profile.Plate,
		"fuel_consumption_l_100km": profile.LPer100Km,
		// Returned so the app can say where the figure came from rather than
		// presenting a class floor as though it were this car's measurement.
		"consumption_source": string(profile.Source),
		"verified":           profile.Verified,
	})
}

// handleListVehicleMakes and handleListVehicleModels back the picker that keeps
// a driver from typing a model name the lookup will not recognise.
func (d Deps) handleListVehicleMakes(w http.ResponseWriter, _ *http.Request) {
	makes, err := vehicles.Makes()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "the vehicle list is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"makes": makes})
}

func (d Deps) handleListVehicleModels(w http.ResponseWriter, r *http.Request) {
	make_ := r.URL.Query().Get("make")
	if make_ == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "make is required")
		return
	}

	entries, err := vehicles.Models(make_)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "the vehicle list is unavailable")
		return
	}

	// An empty list is a real answer, not a 404: most cars in the prefecture
	// are older than the data, and the app's next step is to ask for the CO2
	// figure rather than to report that the make does not exist.
	out := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		out = append(out, map[string]any{
			"model": e.Model, "fuel": string(e.Fuel), "engine_cc": e.EngineCC,
			"fuel_consumption_l_100km": e.LPer100Km,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"models": out})
}
