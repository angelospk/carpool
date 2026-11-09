# How other carpool apps show the five Flutter v1 features

Research for the Flutter port, 2026-11-09. Web search only (the Grok browser
did not start). Sources are mostly press and third-party pages; no vendor
help page confirmed any detail, so treat each line as "reported", not "verified".

| Feature | BlaBlaCar | Waze Carpool (closed 2022) | Karos | What carpool does |
| --- | --- | --- | --- | --- |
| Live driver position + ETA | «Ride Map»: an hour before departure the driver pins the exact start; passengers see each other and can share their own position ([Clubic](https://www.clubic.com/blablacar/actualite-792948-blablacar-ride-map-retrouver-covoiturage.html)). «Ease Pickup»: passenger sees the driver approach the pickup ([dev.ua](https://dev.ua/en/news/blablacar-ease-pickup-1790680985)). | Driver position and ETA shared with the rider while en route to the pickup ([ToS](https://www.waze.com/ca/carpool/legal/tos)). | No source found. | Car dot + «σε N'» on the passenger's trip map, from `GET /v1/trips/{id}/live` (`driver_eta_min`). Passenger sees the car only inside the live window the server opens. |
| Meeting point + walk | Daily: suggested, editable meeting points on the driver's road ([l-itineraire](https://www.l-itineraire.com/mobilite/covoiturage/blablacar-daily-avis-guide)). | Ride offer shows walking to the pickup and from the drop-off ([NBC Bay Area](https://www.nbcbayarea.com/news/local/google-tests-free-carpooling-app-waze-rider-in-bay-area/74599/?amp=1)). | Roadside meeting points on the driver's route, to keep detours small ([okvoyage](https://www.okvoyage.com/post/application-mobile-covoiturage/), low trust). | Pickup pin from `stops[]`; dashed walking line from the passenger to the pin. No foot routing on the server, so the line is straight and labelled as such, with a «Οδηγίες» hand-off to the maps app. |
| Detour before accept | No source found. | Riders ranked by closeness of pickup/drop-off; detour called "minimal", no figure ([Medium](https://medium.com/waze/your-commute-is-about-to-get-better-2ce456428c6f)). | Detour minimised by meeting-point choice. | Ask screen leads with «παράκαμψη +N'» and +km from `GET /v1/asks/{id}` before the two buttons. |
| Share trip with family | Third-party listing names sharing ride location with friends/family ([webcatalog](https://webcatalog.io/apps/blablacar/mobile)); unconfirmed. Uber's «Share My Trip» is the common pattern: up to 5 contacts get a live link. | No source found. | No source found. | No trip-share endpoint exists. Option A (no backend, what Expo already does in `app/src/design/tripShare.ts`): OS share sheet with driver, car, plate and time. Option B: a live public link, needs a backend endpoint. Harold decides. |
| Route preview + alternatives | Driver plans the route; tolls drive price and route choice ([The Points Guy](https://thepointsguy.com/2017/03/money-saving-tips-bla-bla-car)). No source for an alternatives picker. | n/a | n/a | No preview endpoint: `POST /v1/estimates` returns km/min only, and `POST /v1/routes` lets the server pick the road. Alternatives need a backend change (OSRM `alternatives=true` + a chosen-route field). Harold decides. |

## Takeaways for the design

1. Every app shows the car only near the pickup time, never all day. The
   server's live window already does this.
2. The detour number is the driver's main decision input. Put it first, in
   big type.
3. Walking to the meeting point is shown as distance or time, not as a route.
   A straight dashed line with a metre figure is enough for v1.
4. Trip sharing in the wild means a live link (Uber). A static text share is
   the honest v1 without a backend change.
