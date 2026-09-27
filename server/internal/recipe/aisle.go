package recipe

import "strings"

// aisleKeywords maps slug fragments (accent-free, HU and EN) to an aisle. Order
// matters: the first match wins, so specific words come before generic ones
// ("kokusztej" must hit pantry before "tej" hits dairy).
var aisleKeywords = []struct {
	aisle string
	words []string
}{
	{"household", []string{"szemeteszsak", "trash_bag", "bin_bag", "wc_papir", "toilet_paper", "papirtorlo", "paper_towel", "mosogatoszer", "dish_soap", "mososzer", "detergent", "alufolia", "foil", "folia", "szalveta", "napkin", "szivacs", "sponge", "elem", "battery"}},
	{"canned", []string{"konzerv", "canned", "kukorica", "corn", "passata", "paradicsomsuritmeny", "tomato_paste", "sweetcorn"}},
	{"pantry", []string{"kokusztej", "coconut_milk", "liszt", "flour", "cukor", "sugar", "tarhonya", "teszta", "pasta", "rizs", "rice", "zsemlemorzsa", "breadcrumb", "sutopor", "baking_powder", "szodabikarbona", "soda", "eleszto", "yeast", "olaj", "oil", "ecet", "vinegar", "mez", "honey", "kakao", "cocoa", "csokolade", "chocolate", "nutella", "zabpehely", "oat", "mak", "poppy", "szezam", "sesame", "tokmag", "pumpkin_seed", "napraforgo", "sunflower", "len", "flax", "dio", "walnut", "mogyoro", "hazelnut", "mandula", "almond", "vanilia", "vanilla", "leveles_teszta", "puff_pastry", "alaple", "stock", "broth", "keksz", "biscuit", "puding", "pudding", "zselatin", "gelatin", "szirup", "syrup", "karamell", "caramel", "marshmallow"}},
	{"spices", []string{"so", "salt", "bors", "pepper", "paprikapor", "pirospaprika", "paprika_powder", "fahej", "cinnamon", "komeny", "cumin", "caraway", "oregano", "bazsalikom", "basil", "kakukkfu", "thyme", "rozmaring", "rosemary", "szerecsendio", "nutmeg", "gyomber", "ginger", "chili", "curry", "mustar", "mustard", "ketchup", "majonez", "mayo", "szojaszosz", "soy", "fuszer", "spice", "babérlevel", "baberlevel", "bay", "szegfuszeg", "clove_spice", "kurkuma", "turmeric", "tabasco", "worcester", "sriracha", "pesto", "salsa"}},
	{"dairy", []string{"tojas", "egg", "tej", "milk", "tejfol", "sour_cream", "tejszin", "cream", "vaj", "butter", "sajt", "cheese", "parmezan", "parmesan", "mozzarella", "turo", "cottage", "quark", "joghurt", "yogurt", "juhturo", "kremsajt", "cream_cheese", "mascarpone", "feta", "margarin", "margarine"}},
	{"meat", []string{"csirke", "chicken", "marha", "beef", "sertes", "pork", "bacon", "sonka", "ham", "kolbasz", "sausage", "szalami", "salami", "parizsi", "bologna", "virsli", "hot_dog", "daralt", "minced", "ground", "zuza", "gizzard", "maj", "liver", "pulyka", "turkey", "kacsa", "duck", "hal", "fish", "lazac", "salmon", "tonhal", "tuna", "garnela", "shrimp", "comb", "thigh", "mell", "breast", "labszar", "shank", "karaj", "loin", "tarja", "szalonna", "husz", "hus", "meat"}},
	{"produce", []string{"hagyma", "onion", "fokhagyma", "garlic", "burgonya", "krumpli", "potato", "paradicsom", "tomato", "paprika", "bell_pepper", "repa", "carrot", "zeller", "celery", "petrezselyem", "parsley", "kapor", "dill", "snidling", "metelohagyma", "chive", "gomba", "mushroom", "karfiol", "cauliflower", "brokkoli", "broccoli", "kaposzta", "cabbage", "cukkini", "zucchini", "padlizsan", "eggplant", "uborka", "cucumber", "salata", "lettuce", "spenot", "spinach", "tok", "pumpkin", "alma", "apple", "korte", "pear", "citrom", "lemon", "lime", "narancs", "orange", "banan", "banana", "eper", "strawberry", "malna", "raspberry", "afonya", "blueberry", "szolo", "grape", "menta", "mint", "avokado", "avocado", "koriander", "cilantro", "rukkola", "arugula", "retek", "radish", "borso", "pea", "bab", "bean"}},
	{"bakery", []string{"kenyer", "bread", "zsemle", "roll", "kifli", "croissant", "bagett", "baguette", "tortilla", "pita", "kalacs", "brioche"}},
	{"frozen", []string{"fagyasztott", "frozen", "jegkrem", "ice_cream", "jeg", "ice"}},
	{"drinks", []string{"bor", "feherbor", "vorosbor", "wine", "sor", "beer", "vodka", "rum", "gin", "whisky", "tequila", "likor", "liqueur", "palinka", "brandy", "pezsgo", "champagne", "prosecco", "szoda", "soda_water", "tonic", "cola", "ust", "juice", "le_", "gyumolcsle", "kave", "coffee", "tea", "limonade", "sprite", "energiaital", "grenadine", "gyomber_sor", "ginger_beer", "ginger_ale"}},
}

// AisleKeyword is one aisle's keyword list, exported so the app can guess
// aisles offline with the same table.
type AisleKeyword struct {
	Aisle string   `json:"aisle"`
	Words []string `json:"words"`
}

func AisleKeywords() []AisleKeyword {
	out := make([]AisleKeyword, len(aisleKeywords))
	for i, g := range aisleKeywords {
		out[i] = AisleKeyword{g.aisle, g.words}
	}
	return out
}

// GuessAisle picks a grocery aisle from an ingredient's name. It is a
// suggestion for new ingredients only; a category the user set always wins.
func GuessAisle(name L) string {
	for _, lang := range Langs {
		s := "_" + Slug(name[lang], "_") + "_"
		if s == "__" {
			continue
		}
		for _, group := range aisleKeywords {
			for _, w := range group.words {
				// Whole-word for short words ("so", "bor", "tej"), prefix match
				// otherwise, so Hungarian suffixes still hit ("csirkecomb").
				if len(w) <= 3 {
					if strings.Contains(s, "_"+w+"_") {
						return group.aisle
					}
				} else if strings.Contains(s, "_"+w) || strings.Contains(s, w) && len(w) >= 5 {
					return group.aisle
				}
			}
		}
	}
	return "other"
}
