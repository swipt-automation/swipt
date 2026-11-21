//go:build cgo && (darwin || android)

package graphics

import "math"

func (r rect) intersection(other rect) (rect, bool) {
	left, top := max(r.x, other.x), max(r.y, other.y)
	right, bottom := min(r.maxX(), other.maxX()), min(r.maxY(), other.maxY())
	if right < left || bottom < top {
		return rect{}, false
	}
	return rect{left, top, right - left, bottom - top}, true
}

func translation(tx, ty float64) affine {
	return affine{a: 1, d: 1, tx: tx, ty: ty}
}

func rotation(angle float64) affine {
	cosine, sine := math.Cos(angle), math.Sin(angle)
	return affine{a: cosine, b: sine, c: -sine, d: cosine}
}

func (t affine) then(next affine) affine {
	return affine{
		a:  t.a*next.a + t.b*next.c,
		b:  t.a*next.b + t.b*next.d,
		c:  t.c*next.a + t.d*next.c,
		d:  t.c*next.b + t.d*next.d,
		tx: t.tx*next.a + t.ty*next.c + next.tx,
		ty: t.tx*next.b + t.ty*next.d + next.ty,
	}
}

func (t affine) inverse() affine {
	determinant := t.a*t.d - t.b*t.c
	if determinant == 0 {
		return t
	}
	return affine{
		a:  t.d / determinant,
		b:  -t.b / determinant,
		c:  -t.c / determinant,
		d:  t.a / determinant,
		tx: (t.c*t.ty - t.d*t.tx) / determinant,
		ty: (t.b*t.tx - t.a*t.ty) / determinant,
	}
}
