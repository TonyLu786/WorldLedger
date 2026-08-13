package org.worldledger.fabric.capture;

public final class CaptureSequence {
	private long next;

	public CaptureSequence() {
		this(1);
	}

	CaptureSequence(long first) {
		if (first < 0 || first == Long.MAX_VALUE) {
			throw new IllegalArgumentException("invalid first capture sequence");
		}
		this.next = first;
	}

	public synchronized long reserve() {
		if (next == Long.MAX_VALUE) {
			throw new IllegalStateException("capture sequence exhausted");
		}
		return next++;
	}
}
