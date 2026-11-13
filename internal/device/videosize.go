package device

import "github.com/swipt-automation/swipt/internal/rfb/media"

const largestPicture = 16384

// In modes 5, 7 and 9, the device's streams put VPS/SPS/PPS before each IDR (iPadOS 27.2 24B5099f).
func (v *videoSource) checkPictureSize(sequence *media.HEVCSequence) {
	width, height := sequence.CodedWidth, sequence.CodedHeight
	if width > largestPicture || height > largestPicture {
		return
	}
	if v.pictureWidth == 0 {
		v.pictureWidth, v.pictureHeight = width, height
		v.connection.logger.Debug("video size in its parameter sets: ", width, "x", height, ", started at ", v.info.Width, "x", v.info.Height)
		return
	}
	if width == v.pictureWidth && height == v.pictureHeight {
		return
	}
	v.pictureWidth, v.pictureHeight = width, height
	v.connection.logger.Info("the device's video changed size: ", width, "x", height)
	v.connection.checkDisplay()
	v.connection.videoSize(width, height)
}
