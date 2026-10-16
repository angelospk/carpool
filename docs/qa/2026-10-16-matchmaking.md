# QA 16 Οκτωβρίου 2026: matchmaking με δύο personas

Δύο πράκτορες έπαιξαν χρήστες στο ίδιο τοπικό stack (βάση `qa1016`, API `:8187`):

- **Οδηγός** (Luna, `gpt-6-luna`), web `:8197`: Κώστας από Ορεστιάδα και Ελένη από Κομοτηνή.
  Σημειώσεις: [2026-10-16-driver.md](2026-10-16-driver.md).
- **Επιβάτης** (Sonnet 5.5), web `:8198`: Σοφία, φοιτήτρια, και μια δεύτερη επιβάτισσα.
  Σημειώσεις: [2026-10-16-passenger.md](2026-10-16-passenger.md).

Ο οδηγός δημοσίευσε 3 διαδρομές. Ο επιβάτης έστειλε περίπου 60 αιτήματα: ταίριασμα,
χωριά πάνω και έξω από τη διαδρομή, αντίθετη κατεύθυνση, λάθος μέρα και ώρα, όρια παραθύρου,
θέσεις, αποσκευές, women_only, block, και ανταγωνισμό για 1 θέση. Οι κανόνες του
MATCHING.md ισχύουν όπως γράφονται. Τα προβλήματα είναι στα όρια και στο τι βλέπει ο επιβάτης.

## Διορθώθηκαν σε αυτό το πέρασμα

| # | Τι | Διόρθωση | Test |
|---|---|---|---|
| B1 | Η ακύρωση αιτήματος με ζωντανό ask δεν ξυπνούσε όσους περίμεναν. Σε διαδρομή 1 θέσης ο επόμενος περίμενε έως 1 ώρα. | `handleCancelRequest`: transaction, και `outbox.WakeAll` για τις αναχωρήσεις των asks που ακυρώθηκαν | `TestCancellingARequestWakesWhoeverWaitsForItsTrip`, `TestCancellingAnOpenRequestWakesNothing` |
| B2 | Ένας επιβάτης με δύο αιτήματα (1 και 2 θέσεων) κρατούσε όλες τις θέσεις μιας αναχώρησης. | Stage 1: καμία αναχώρηση που επικαλύπτει χρονικά αναχώρηση όπου ο επιβάτης έχει confirmed booking ή pending ask | `TestAPassengerAlreadyBeingAskedAboutATripIsNotOfferedItAgain` |
| B3 | Ίδια διαδρομή δημοσιευμένη δύο φορές: ο επιβάτης με κράτηση στη μία ρωτιόταν για την άλλη, την ίδια ώρα. | Ο ίδιος κανόνας επικάλυψης | `TestAPassengerIsNotOfferedATripThatOverlapsOneTheyHave`, `TestWhatNoLongerHoldsThePassengerDoesNotBlockAnOverlappingTrip`, `TestASeatOnAnEarlierTripDoesNotBlockALaterOne` |
| D1 | Edit διαδρομής με κράτηση: η παλιά (retired) αναχώρηση κρατά την κράτηση, αλλά έπαιρνε και νέους επιβάτες. Μαζί με τη νέα έκδοση, ο οδηγός είχε περισσότερες θέσεις από όσες δήλωσε. | Stage 1 και `Waiting`: `t.active` | `TestARetiredRouteTakesNoNewPassengers`, `TestARetiredRouteWakesNobody` |
| U2 | Το «Πρωί» (06–10) δεν έβρισκε οδηγό που φεύγει 10:00. Η σύγκριση ήταν αυστηρή. | Stage 1 και `Waiting`: `<=` / `>=` | `TestADepartureOnTheEdgeOfTheWindowMatches` |
| U5 | Μετά από άρνηση ή λήξη: «Ψάχνουμε επόμενο οδηγό. 1 οδηγός δεν μπορούσε.», ενώ δεν υπήρχε επόμενος οδηγός. | Νέο μήνυμα `home.requestExhausted` όταν όλοι οι οδηγοί του δρόμου πέρασαν. «δεν μπορούσε» → «είπε όχι ή δεν απάντησε». | `OpenRequests.test.tsx` |

Μετά από δύο κύκλους review του Codex (2 γύροι ο καθένας) διορθώθηκαν και τα εξής:

- Κοινό advisory lock ανά επιβάτη (`match.PassengerLockSQL`) σε `createAsk`, `Accept` και `standing.Approve`. Και τα τρία το παίρνουν πριν από κάθε row lock και ελέγχουν ξανά την επικάλυψη κάτω από αυτό. Έτσι:
  - δύο αιτήματα του ίδιου επιβάτη που ταιριάζουν ταυτόχρονα δεν παίρνουν και τα δύο ask,
  - ένα accept την ώρα που λήγει το ask δεν δίνει δεύτερη κράτηση,
  - μια μόνιμη κράτηση δεν κλείνει εβδομάδα όπου ο επιβάτης ταξιδεύει ήδη με άλλον.
- Το `Approve` έπαιρνε το lock του επιβάτη μετά το lock της μόνιμης κράτησης. Μαζί με ένα `Accept` και μια αναστολή λογαριασμού αυτό έκανε deadlock. Το test `TestApprovalDoesNotHoldTheArrangementWhileWaitingForThePassenger` το αναπαράγει (`40P01`) με την παλιά σειρά.
- Το `createAsk` διαβάζει το `t.active` μετά το lock της αναχώρησης, σε δική του εντολή.
- Ο `drivers_untried` μετρά ανά αναχώρηση: οδηγός που αρνήθηκε μετράει ξανά αν έχει άλλη διαδρομή, ή αναχώρηση στο ίδιο παράθυρο που δεν έφυγε ακόμα και δεν ρωτήθηκε.
- Η ακύρωση αιτήματος αφήνει τη σύνδεση πριν το `reportCancelRace`.

Τα tests των locks ρωτούν τη βάση (`pg_stat_activity`) αν η κλήση περιμένει. Δεν βασίζονται σε sleep. Αν αφαιρέσεις κάθε lock, το αντίστοιχο test αποτυγχάνει.

Το MATCHING.md γράφει τους νέους κανόνες.

## Ανοιχτά: χρειάζονται απόφαση προϊόντος

Σειρά προτεραιότητας:

1. **U4: σημείο παραλαβής.** Η αφετηρία είναι πάντα το κέντρο του οικισμού. Οι Φέρες απέχουν 4 km από τη διαδρομή Ορεστιάδα→Αλεξ/πολη και δεν ταιριάζουν ποτέ (ακτίνα 2 km). Πρόταση: ο επιβάτης να διαλέγει σημείο ή στάση, ή το pickup να πηγαίνει στο πλησιέστερο σημείο του δρόμου.
2. **U3: λόγος για «δεν ταίριαξε».** Κάθε αποτυχία λέει «Δεν ρωτήθηκε κανένας οδηγός ακόμα». Πρόταση: το API να δίνει `reason` (`no_departure_that_day`, `outside_radius`, `over_detour_budget`, `seats_held`, `queued`).
3. **U1: μόνο «Σήμερα/Αύριο» και 4 ζώνες.** Από το UI δεν γίνεται αίτημα για Δευτέρα. Το API δέχεται έως 3 εβδομάδες.
4. **Budget παράκαμψης.** Τα 2 λεπτά dwell ανά στάση μετράνε στο budget. Με 10 λεπτά budget μένουν 6 λεπτά δρόμου. Ο οδηγός βλέπει «3,6 λ.» ενώ ζυγίστηκαν 7,6. Θέλει απόφαση: να μη μετράει ο dwell, ή να φαίνεται.

## Δεύτερο πέρασμα (17 Οκτωβρίου)

| # | Τι | Διόρθωση | Test |
|---|---|---|---|
| Edit | Η «Αλλαγή» δημοσίευε νέα διαδρομή και απέσυρε την παλιά. Τη μέρα με κράτηση ο οδηγός είχε δύο αναχωρήσεις. | `PUT /v1/me/routes/{id}`: αλλαγή της ίδιας διαδρομής. Η αναχώρηση με κράτηση μένει όπως συμφωνήθηκε, οι άλλες αλλάζουν. Άλλα άκρα: 409 `route_ends_changed`, και το app δημοσιεύει νέα. | `TestEditingARouteKeepsTheBookedDepartureAsAgreed`, `editRoute.test.tsx` |
| Νέο | Αναχώρηση που ακύρωσε ο οδηγός γύριζε σε `scheduled` στο επόμενο tick του materializer, και έπαιρνε επιβάτες. | `CancelOccurrence`: η μέρα μπαίνει στα `exceptions`, κάτω από το lock του materializer. | `TestACalledOffDepartureStaysCalledOff` |
| Review | Ο materializer μπορούσε να αλλάξει ώρα σε αναχώρηση την ώρα που γραφόταν ask (snapshot πριν το lock). Το edit έσβηνε τις ακυρωμένες μέρες. | Ο materializer κλειδώνει πρώτα τις μελλοντικές αναχωρήσεις. Το edit κρατά τα `exceptions`. | `TestARunWaitingForADepartureSeesTheAskThatHeldIt`, `TestAnEditKeepsTheDaysTheDriverCalledOff` |
| Διπλή | Η ίδια διαδρομή δημοσιευόταν δύο φορές. | 409 `duplicate_route` (ίδια άκρα, μέρες, ώρες), με lock ανά οδηγό για το διπλό tap. | `TestTheSameRouteCannotBePublishedTwice`, `TestAnEditCannotCopyAnotherRoute` |
| B4 | Το `/alternatives` έδειχνε αυτοκίνητο που το stage 2 απορρίπτει. | Περνά και από το stage 2, όπως το `AskNext`. | `TestAlternativesListOnlyCarsTheRoutedCheckAccepts` |
| B5 | Σε 4 ταυτόχρονα ίδια POST, κάποιο απαντούσε `asking:false`. | Αν το `AskNext` δεν βρει κανέναν, ξαναδιαβάζει το ζωντανό ask. | `TestCopiesOfOnePostAllSayADriverIsBeingAsked` |
| U6 | Ο επιβάτης δεν έβλεπε ως πότε απαντά ο οδηγός. | `ask_expires_at`· «Απαντά έως τις 10:42.» | `TestMyRequestsCountsDriversAndLiveAsks`, `OpenRequests.test.tsx` |
| U7 | Ακύρωση αιτήματος με ένα tap. | Ερώτηση επιβεβαίωσης. | `request.test.tsx` |
| U12 | Λάθη πληκτρολόγησης και «Ορεστιάδα, Έβρος» δεν έβρισκαν τίποτα· όχι τοπική προτίμηση. | Διόρθωση έως 1–2 γράμματα (από 4 γράμματα και πάνω)· ό,τι είναι μετά το κόμμα αγνοείται· `near_lon/near_lat` από το άλλο άκρο. | `typos_test.go`, `TestPlacesNearTheOtherEndComeFirst`, `PlaceField.test.tsx` |
| U13 | 4 θέσεις σε δρόμο με αυτοκίνητα 3 θέσεων: «δεν ρωτήθηκε κανένας ακόμα». | `most_seats_on_corridor`· «Κανένα αυτοκίνητο … Το πολύ 3 θέσεις.» | `TestTheMostSeatsOnTheRoadAreCounted`, `OpenRequests.test.tsx` |
| Φόρμα | Η «Παράκαμψη έως» ήταν στο τέλος της φόρμας. | Ακριβώς κάτω από τη διαδρομή. | `publishSections.test.tsx` |

## Τι δούλεψε σωστά

Αντίθετη κατεύθυνση, λάθος μέρα, παράθυρα ±30′/±3 ω, θέσεις, αποσκευές, women_only, block/unblock,
ουρά για 1 θέση (ο δεύτερος ρωτήθηκε 10 s μετά τη λήξη του πρώτου ask), trigger 2 (νέα διαδρομή
ξυπνά ανοιχτό αίτημα σε 6–10 s), accept → booking, chat, και κάρτα ταξιδιού και στις δύο πλευρές.
`POST /v1/requests` χρειάζεται 30–130 ms.
