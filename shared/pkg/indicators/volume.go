package indicators

// Volume returns total volume from price-volume pairs.
func Volume(pvs []PriceVolume) float64 {
	sum := 0.0
	for _, pv := range pvs {
		sum += pv.Volume
	}
	return sum
}

// VolumeFromSlice returns sum of volumes.
func VolumeFromSlice(volumes []float64) float64 {
	sum := 0.0
	for _, v := range volumes {
		sum += v
	}
	return sum
}

// VolumeSpike detects if current window volume is a spike relative to baseline (e.g. rolling average).
// currentVol = volume in current window, avgVol = average volume in previous windows.
// threshold = multiplier (e.g. 2.0 = spike when current > 2*avg).
func VolumeSpike(currentVol, avgVol, threshold float64) bool {
	if avgVol <= 0 {
		return false
	}
	return currentVol >= threshold*avgVol
}

// VolumeSpikeRatio returns currentVol/avgVol; 0 if avgVol is 0.
func VolumeSpikeRatio(currentVol, avgVol float64) float64 {
	if avgVol <= 0 {
		return 0
	}
	return currentVol / avgVol
}
