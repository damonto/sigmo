class CallMicProcessor extends AudioWorkletProcessor {
  process(inputs) {
    const channel = inputs[0]?.[0]
    if (channel && channel.length > 0) {
      const copy = new Float32Array(channel.length)
      copy.set(channel)
      this.port.postMessage({ type: 'pcm', samples: copy, frame: currentFrame }, [copy.buffer])
    }
    return true
  }
}

registerProcessor('sigmo-call-mic', CallMicProcessor)
