export class ResizeObserverEntry {
    target;
    contentRect;
    constructor(target, contentRect) {
        this.target = target;
        this.contentRect = contentRect;
    }
}
export default class ResizeObserver {
    callback;
    observedElements = new Set();
    constructor(callback) {
        this.callback = callback;
    }
    observe(element) {
        if (this.observedElements.has(element)) {
            return;
        }
        this.observedElements.add(element);
        element.resizeObservers ||= new Set();
        element.resizeObservers.add(this);
        let lastMeasuredSize = element.internal_lastMeasuredSize;
        if (lastMeasuredSize === undefined && element.yogaNode) {
            const width = element.yogaNode.getComputedWidth();
            const height = element.yogaNode.getComputedHeight();
            lastMeasuredSize = { width, height };
            element.internal_lastMeasuredSize = lastMeasuredSize;
        }
        if (lastMeasuredSize) {
            const entry = new ResizeObserverEntry(element, lastMeasuredSize);
            try {
                this.callback([entry], this);
            }
            catch (error) {
                console.error(error);
            }
        }
    }
    unobserve(element) {
        this.observedElements.delete(element);
        element.resizeObservers?.delete(this);
    }
    disconnect() {
        for (const element of this.observedElements) {
            element.resizeObservers?.delete(this);
        }
        this.observedElements.clear();
    }
    // Internal method called by Ink during layout
    internalTrigger(entries) {
        try {
            this.callback(entries, this);
        }
        catch (error) {
            console.error(error);
        }
    }
}
//# sourceMappingURL=resize-observer.js.map