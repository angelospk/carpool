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
      default:
        return 'Κάτι πήγε στραβά';
    }
  }
  if (error is http.ClientException) {
    return 'Δεν βρίσκω τον server στο $apiUrl. Τρέχει, και είσαι στο ίδιο δίκτυο;';
  }
  return 'Κάτι πήγε στραβά';
}
