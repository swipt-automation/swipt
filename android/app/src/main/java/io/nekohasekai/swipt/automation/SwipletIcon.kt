package io.nekohasekai.swipt.automation

import android.graphics.Bitmap
import android.graphics.Paint
import android.graphics.Typeface
import androidx.compose.foundation.Canvas
import androidx.compose.foundation.Image
import androidx.compose.foundation.background
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.size
import androidx.compose.foundation.shape.RoundedCornerShape
import androidx.compose.material3.MaterialTheme
import androidx.compose.runtime.Composable
import androidx.compose.runtime.remember
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.draw.clip
import androidx.compose.ui.graphics.Brush
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.graphics.asImageBitmap
import androidx.compose.ui.graphics.drawscope.drawIntoCanvas
import androidx.compose.ui.graphics.luminance
import androidx.compose.ui.graphics.nativeCanvas
import androidx.compose.ui.graphics.toArgb
import androidx.compose.ui.layout.ContentScale
import androidx.compose.ui.unit.Dp
import io.nekohasekai.swipt.app.SwiptApplication

data class SwipletIcon(val color: String?, val symbol: String, val symbolColor: String?, val image: Bitmap? = null) {
    companion object {
        val default = SwipletIcon(null, "layers", null)

        private const val FONT_ASSET = "MaterialIcons-Regular.ttf"
        private const val CODEPOINTS_ASSET = "MaterialIcons-Regular.codepoints"

        val typeface: Typeface by lazy {
            Typeface.createFromAsset(SwiptApplication.application.assets, FONT_ASSET)
        }

        private val glyphs: Map<String, String> by lazy {
            SwiptApplication.application.assets.open(CODEPOINTS_ASSET).bufferedReader().useLines { lines ->
                buildMap {
                    for (line in lines) {
                        val separator = line.indexOf(' ')
                        if (separator <= 0) continue
                        val code = line.substring(separator + 1).toIntOrNull(16) ?: continue
                        put(line.substring(0, separator), String(Character.toChars(code)))
                    }
                }
            }
        }

        private val colors = mapOf(
            "black" to 0xFF000000,
            "white" to 0xFFFFFFFF,
            "gray" to 0xFF9E9E9E,
            "red" to 0xFFF44336,
            "orange" to 0xFFFF9800,
            "yellow" to 0xFFFFEB3B,
            "green" to 0xFF4CAF50,
            "mint" to 0xFF1DE9B6,
            "teal" to 0xFF009688,
            "cyan" to 0xFF00BCD4,
            "blue" to 0xFF2196F3,
            "indigo" to 0xFF3F51B5,
            "purple" to 0xFF9C27B0,
            "pink" to 0xFFE91E63,
            "brown" to 0xFF795548,
        )

        fun glyph(symbol: String): String = glyphs[symbol] ?: glyphs.getValue(default.symbol)

        private fun parseColor(value: String): Color? {
            colors[value]?.let { return Color(it) }
            if (value.length != 7 || value[0] != '#') return null
            return value.substring(1).toLongOrNull(16)?.let { Color(0xFF000000 or it) }
        }
    }

    internal fun tileColor(accent: Color): Color = color?.let(::parseColor) ?: accent

    internal fun symbolTint(tile: Color): Color = symbolColor?.let(::parseColor) ?: if (tile.luminance() > 0.5f) Color.Black else Color.White
}

@Composable
fun SwipletIconImage(icon: SwipletIcon, size: Dp, modifier: Modifier = Modifier) {
    val shape = RoundedCornerShape(size * 0.225f)
    val image = icon.image
    if (image != null) {
        val bitmap = remember(image) { image.asImageBitmap() }
        Image(bitmap, contentDescription = null, modifier = modifier.size(size).clip(shape), contentScale = ContentScale.Crop)
        return
    }
    val tile = icon.tileColor(MaterialTheme.colorScheme.primary)
    val tint = icon.symbolTint(tile)
    val glyph = remember(icon.symbol) { SwipletIcon.glyph(icon.symbol) }
    val paint = remember {
        Paint(Paint.ANTI_ALIAS_FLAG).apply {
            typeface = SwipletIcon.typeface
            textAlign = Paint.Align.CENTER
        }
    }
    Box(
        modifier
            .size(size)
            .background(tile, shape)
            .background(Brush.verticalGradient(listOf(Color.White.copy(alpha = 0.14f), Color.Transparent)), shape),
        contentAlignment = Alignment.Center,
    ) {
        Canvas(Modifier.size(size * 0.6f)) {
            paint.color = tint.toArgb()
            paint.textSize = this.size.height
            drawIntoCanvas { canvas ->
                canvas.nativeCanvas.drawText(glyph, this.size.width / 2f, this.size.height, paint)
            }
        }
    }
}
