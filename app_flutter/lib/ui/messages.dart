import 'package:http/http.dart' as http;

import '../config.dart';
import '../data/api.dart';

/// What to tell somebody when a call failed. The server refuses with a code,
/// and the code is what decides the sentence: a generic "κάτι πήγε στραβά"
/// for a missing invite code would leave the person with nothing to do.
String failureText(Object error) {
  if (error is ApiException) {
    switch (error.code) {
      case 'invalid_code':
        return 'Λάθος ή ληγμένος κωδικός';
      case 'account_suspended':
        return 'Ο λογαριασμός σου δεν μπορεί να χρησιμοποιηθεί. Γράψε μας αν νομίζεις ότι είναι λάθος.';
      case 'rate_limited':
        return 'Πολλές προσπάθειες. Δοκίμασε σε λίγο.';
      case 'invalid_token':
        return 'Πέρασε η ώρα. Ζήτα νέο κωδικό και ξεκίνα ξανά.';
      case 'unknown_place':
        return 'Δεν βρήκα αυτόν τον οικισμό. Διάλεξε έναν από τη λίστα.';
      case 'underage':
        return 'Η εφαρμογή είναι για ενήλικες. Πρέπει να είσαι 18 ετών.';
      case 'invalid_birth_date':
        return 'Χρειάζεται ημερομηνία γέννησης.';
      case 'invalid_name':
        return 'Αυτό το όνομα δεν μπορεί να χρησιμοποιηθεί.';
      case 'invite_required':
        return 'Χρειάζεται κωδικός πρόσκλησης. Θα τον βρεις στη σελίδα της διαδρομής που σου έστειλαν.';
      case 'invite_invalid':
        return 'Αυτός ο κωδικός πρόσκλησης δεν ισχύει ή τελείωσε.';
      case 'already_matched':
        return 'Ένας οδηγός ανέλαβε τη διαδρομή σου.';
      case 'invalid_request':
        return 'Έλεγξε τα στοιχεία και δοκίμασε ξανά.';
      case 'unauthorized':
        return 'Η σύνδεση έληξε. Μπες ξανά.';
      case 'outside_window':
        return 'Είναι νωρίς για αναχώρηση. Ξεκίνα μέσα στην ώρα που δήλωσες.';
      case 'bad_departure':
        return 'Η αναχώρηση πρέπει να είναι μέσα στο επόμενο τέταρτο.';
      case 'drive_departed':
      case 'departed':
        return 'Η διαδρομή έχει ήδη ξεκινήσει.';
      case 'trip_not_running':
        return 'Αυτή η διαδρομή ακυρώθηκε.';
      case 'not_departed':
        return 'Η διαδρομή δεν έχει ξεκινήσει ακόμα.';
      case 'ask_expired':
        return 'Η ερώτηση έληξε.';
      case 'ask_closed':
        return 'Η ερώτηση έκλεισε: την απάντησες ή την πήρε άλλος οδηγός.';
      case 'not_found':
        return 'Δεν βρέθηκε. Ίσως ακυρώθηκε.';
      case 'outside_country':
        return 'Το σημείο είναι εκτός Ελλάδας.';
      case 'outside_service_area':
        return 'Δεν μπορώ να βρω δρόμο ως εκεί ακόμα.';
      case 'duplicate_route':
        return 'Έχεις ήδη δημοσιεύσει αυτή τη διαδρομή.';
      case 'routing_failed':
        return 'Δεν βρήκα δρόμο. Δοκίμασε ξανά σε λίγο.';
      case 'passengers_only':
        return 'Μόνο ο επιβάτης μοιράζεται τη διαδρομή.';
      default:
        return 'Κάτι πήγε στραβά';
    }
  }
  if (error is http.ClientException) {
    return 'Δεν βρίσκω τον server στο $apiUrl. Τρέχει, και είσαι στο ίδιο δίκτυο;';
  }
  return 'Κάτι πήγε στραβά';
}
