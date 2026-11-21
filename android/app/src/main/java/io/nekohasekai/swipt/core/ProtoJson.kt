package io.nekohasekai.swipt.core

import com.google.protobuf.ListValue
import com.google.protobuf.NullValue
import com.google.protobuf.Struct
import com.google.protobuf.Value
import kotlinx.serialization.json.Json
import kotlinx.serialization.json.JsonArray
import kotlinx.serialization.json.JsonElement
import kotlinx.serialization.json.JsonNull
import kotlinx.serialization.json.JsonObject
import kotlinx.serialization.json.JsonPrimitive
import kotlinx.serialization.json.booleanOrNull
import kotlinx.serialization.json.doubleOrNull

internal object ProtoJson {
    fun value(json: String): Value = value(Json.parseToJsonElement(json))

    private fun value(element: JsonElement): Value = when (element) {
        is JsonNull -> Value.newBuilder().setNullValue(NullValue.NULL_VALUE).build()
        is JsonObject -> Value.newBuilder().setStructValue(Struct.newBuilder().putAllFields(element.mapValues { value(it.value) })).build()
        is JsonArray -> Value.newBuilder().setListValue(ListValue.newBuilder().addAllValues(element.map(::value))).build()
        is JsonPrimitive -> when {
            element.isString -> Value.newBuilder().setStringValue(element.content).build()
            element.booleanOrNull != null -> Value.newBuilder().setBoolValue(element.booleanOrNull!!).build()
            else -> Value.newBuilder().setNumberValue(element.doubleOrNull ?: throw IllegalArgumentException("${element.content} is not a number")).build()
        }
    }

    fun json(value: Value): String = element(value).toString()

    fun element(struct: Struct): JsonObject = JsonObject(struct.fieldsMap.mapValues { element(it.value) })

    private fun element(value: Value): JsonElement = when (value.kindCase) {
        Value.KindCase.STRUCT_VALUE -> JsonObject(value.structValue.fieldsMap.mapValues { element(it.value) })
        Value.KindCase.LIST_VALUE -> JsonArray(value.listValue.valuesList.map(::element))
        Value.KindCase.STRING_VALUE -> JsonPrimitive(value.stringValue)
        Value.KindCase.BOOL_VALUE -> JsonPrimitive(value.boolValue)
        Value.KindCase.NUMBER_VALUE -> value.numberValue.let { if (it % 1.0 == 0.0 && kotlin.math.abs(it) < 1e15) JsonPrimitive(it.toLong()) else JsonPrimitive(it) }
        Value.KindCase.NULL_VALUE, Value.KindCase.KIND_NOT_SET, null -> JsonNull
    }
}
