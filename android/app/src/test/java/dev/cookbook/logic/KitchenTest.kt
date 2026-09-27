package dev.cookbook.logic

import dev.cookbook.data.Ingredient
import dev.cookbook.data.UnitDef
import org.junit.Assert.assertEquals
import org.junit.Test

class KitchenTest {
	private val units = listOf(
		UnitDef("g", mapOf("hu" to "g", "en" to "g"), "mass", 1.0, "metric"),
		UnitDef("dkg", mapOf("hu" to "dkg", "en" to "dag"), "mass", 10.0, "metric"),
		UnitDef("kg", mapOf("hu" to "kg", "en" to "kg"), "mass", 1000.0, "metric"),
		UnitDef("oz", mapOf("hu" to "uncia", "en" to "oz"), "mass", 28.3495, "imperial"),
		UnitDef("lb", mapOf("hu" to "font", "en" to "lb"), "mass", 453.592, "imperial"),
		UnitDef("ml", mapOf("hu" to "ml", "en" to "ml"), "volume", 1.0, "metric"),
		UnitDef("dl", mapOf("hu" to "dl", "en" to "dl"), "volume", 100.0, "metric"),
		UnitDef("l", mapOf("hu" to "l", "en" to "l"), "volume", 1000.0, "metric"),
		UnitDef("tsp", mapOf("hu" to "tk", "en" to "tsp"), "volume", 5.0, "both"),
		UnitDef("tbsp", mapOf("hu" to "ek", "en" to "tbsp"), "volume", 15.0, "both"),
		UnitDef("cup", mapOf("hu" to "csésze", "en" to "cup"), "volume", 240.0, "imperial"),
		UnitDef("pc", mapOf("hu" to "db", "en" to "pc"), "count"),
	).associateBy { it.id }

	private val beef = Ingredient("beef_shank", 500.0, unitId = "g", name = mapOf("hu" to "marhalábszár", "en" to "beef shank"))
	private val egg = Ingredient("tojas", 3.0, unitId = "pc", name = mapOf("hu" to "tojás", "en" to "egg"))
	private val salt = Ingredient("so", null, name = mapOf("hu" to "só"))

	@Test fun specExampleScalesFrom4To6() {
		val k = Kitchen(units, "en", false, 6.0 / 4.0)
		val text = k.resolve("Cut the {{beef_shank.qty}}{{beef_shank.unit}} of {{beef_shank.name}} into cubes.", listOf(beef))
		assertEquals("Cut the 750g of beef shank into cubes.", text)
	}

	@Test fun hungarianDecimalComma() {
		val k = Kitchen(units, "hu", false, 0.5)
		assertEquals("1½ db", k.amount(egg)) // counts use fractions
		val flour = Ingredient("liszt", 2.5, unitId = "dl", name = mapOf("hu" to "liszt"))
		assertEquals("1,25 dl", Kitchen(units, "hu", false, 0.5).amount(flour))
	}

	@Test fun toTasteIsNeverScaled() {
		val k = Kitchen(units, "hu", false, 3.0)
		assertEquals("", k.amount(salt))
		assertEquals("só", k.resolve("{{so}}", listOf(salt)))
	}

	@Test fun imperialConversion() {
		val k = Kitchen(units, "en", true, 1.0)
		assertEquals("1 lb", k.amount(beef.copy(qty = 453.592)))
		assertEquals("2 cup", k.amount(Ingredient("milk", 4.8, unitId = "dl", name = mapOf("en" to "milk"))))
		// Spoons are used in both systems: unchanged.
		assertEquals("2 tbsp", k.amount(Ingredient("oil", 2.0, unitId = "tbsp", name = mapOf("en" to "oil"))))
		// 180 °C = 356 °F, rounded to the 5 °F steps oven dials use.
		assertEquals("355 °F", k.temperature(180))
	}

	@Test fun backToMetric() {
		val k = Kitchen(units, "en", false, 1.0)
		assertEquals("4.8 dl", k.amount(Ingredient("milk", 2.0, unitId = "cup", name = mapOf("en" to "milk"))))
	}

	@Test fun rangesScaleBothEnds() {
		val k = Kitchen(units, "hu", false, 2.0)
		val cheese = Ingredient("sajt", 10.0, qtyMax = 15.0, unitId = "dkg", name = mapOf("hu" to "sajt"))
		assertEquals("20–30 dkg", k.amount(cheese))
	}

	@Test fun temperatureToken() {
		assertEquals("Süsd 180 °C-on.", Kitchen(units, "hu", false, 1.0).resolve("Süsd {{temp:180}}-on.", emptyList()))
		assertEquals("Bake at 355 °F.", Kitchen(units, "en", true, 1.0).resolve("Bake at {{temp:180}}.", emptyList()))
	}

	@Test fun timers() {
		val seg = segments("Főzd [120:00] percig, majd [1:30:00] pihen.")
		assertEquals(Segment.Timer(7200, "120:00"), seg[1])
		assertEquals(Segment.Timer(5400, "1:30:00"), seg[3])
		assertEquals("2:00:00", clock(7200))
	}

	@Test fun parsing() {
		assertEquals(2.5 to null, parseQty("2,5"))
		assertEquals(1.5 to null, parseQty("1 1/2"))
		assertEquals(10.0 to 15.0, parseQty("10-15"))
		assertEquals(0.5 to null, parseQty("½"))
		assertEquals(null to null, parseQty("sok"))
	}

	@Test fun foldStripsAccents() {
		assertEquals("gulyasleves tojas", fold("Gulyásleves TOJÁS"))
	}
}
