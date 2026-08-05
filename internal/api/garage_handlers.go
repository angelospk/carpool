package api

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"carpool/internal/drivers"
)

// vehicleResponse is one car as its owner sees it.
//
// The plate is included, because this is the owner's own garage and they know
// their own plate. That is exactly why this helper must not be reused for
// anything a passenger reads: a plate reaches a passenger only after their seat
// has been accepted, and a shared shape is how that rule gets forgotten.
func vehicleResponse(v drivers.Vehicle) map[string]any {
	return map[string]any{
		"id":     v.ID,
		"name":   v.Name(),
		"label":  v.Label,
		"make":   v.Make,
		"model":  v.Model,
		"fuel":   v.Fuel,
		"colour": v.Colour,
		"plate":  v.Plate,
		// What it is priced as drinking, and how firm that number is. Shown to
		// the driver because it is the input to every contribution they will be
		// offered, and they are the one who can correct the car it came from.
		"consumption_l_100km": v.LPer100Km,
		"consumption_source":  string(v.Source),
		"consumption_capped":  v.Capped,
		"is_default":          v.IsDefault,
	}
}

func (d Deps) handleListVehicles(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	live, err := d.Drivers.Vehicles(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "could not read your cars")
		return
	}

	out := make([]map[string]any, 0, len(live))
	for _, v := range live {
		out = append(out, vehicleResponse(v))
	}
	writeJSON(w, http.StatusOK, map[string]any{"vehicles": out})
}

func (d Deps) handleAddVehicle(w http.ResponseWriter, r *http.Request) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return
	}

	var body struct {
		Make          string `json:"make"`
		Model         string `json:"model"`
		Fuel          string `json:"fuel"`
		EngineCC      int    `json:"engine_cc"`
		CO2GramsPerKm int    `json:"co2_g_km"`
		Colour        string `json:"colour"`
		Plate         string `json:"plate"`
		Label         string `json:"label"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	v, err := d.Drivers.AddVehicle(r.Context(), userID, drivers.Car{
		Make: body.Make, Model: body.Model, Fuel: body.Fuel,
		EngineCC: body.EngineCC, CO2GramsPerKm: body.CO2GramsPerKm,
		Colour: body.Colour, Plate: body.Plate, Label: body.Label,
	})
	switch {
	case errors.Is(err, drivers.ErrTooManyVehicles):
		writeError(w, http.StatusConflict, "too_many_vehicles",
			"that is as many cars as one account may keep")
	case errors.Is(err, drivers.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not save the car")
	default:
		writeJSON(w, http.StatusCreated, vehicleResponse(v))
	}
}

func (d Deps) handleSetDefaultVehicle(w http.ResponseWriter, r *http.Request) {
	userID, vehicleID, ok := d.vehicleParams(w, r)
	if !ok {
		return
	}

	err := d.Drivers.SetDefaultVehicle(r.Context(), userID, vehicleID)
	switch {
	case errors.Is(err, drivers.ErrNoVehicle):
		writeError(w, http.StatusNotFound, "not_found", "no such car")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not change the default")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (d Deps) handleRetireVehicle(w http.ResponseWriter, r *http.Request) {
	userID, vehicleID, ok := d.vehicleParams(w, r)
	if !ok {
		return
	}

	err := d.Drivers.RetireVehicle(r.Context(), userID, vehicleID)
	switch {
	case errors.Is(err, drivers.ErrNoVehicle):
		writeError(w, http.StatusNotFound, "not_found", "no such car")
	case errors.Is(err, drivers.ErrVehicleInUse):
		// The order matters and saying so saves a support message: a route
		// naming a car that is gone cannot be priced.
		writeError(w, http.StatusConflict, "vehicle_in_use",
			"a route you have published still uses this car; take the route down first")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "internal", "could not retire the car")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (d Deps) vehicleParams(w http.ResponseWriter, r *http.Request) (uuid.UUID, uuid.UUID, bool) {
	userID, ok := callerID(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthorized", "a bearer token is required")
		return uuid.Nil, uuid.Nil, false
	}
	vehicleID, err := uuid.Parse(chiURLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "the car id must be a uuid")
		return uuid.Nil, uuid.Nil, false
	}
	return userID, vehicleID, true
}
