import { LitElement, html, css } from 'lit';
import { customElement } from 'lit/decorators.js';

export const VERSION = '1.0.0';

export class Knob extends LitElement {
  static styles = css`
    .knob { width: 100px; height: 100px; }
  `;

  private last_ = 0;
  private delta = 0;
  private enabled = true;

  connectedCallback() {
    super.connectedCallback();
    this.last_ = 0;
  }

  disconnectedCallback() {
    super.disconnectedCallback();
  }

  updated() {
    this.updateDelta();
  }

  private updateDelta() {
    if (this.enabled) {
      this.last_ += this.delta;
      this.notify_value(this.last_.toString());
    }
  }

  notify_value(value) {
    this.dispatchEvent(new CustomEvent('change', { detail: { value } }));
  }

  render() {
    return html`<div class="knob">${this.last_}</div>`;
  }
}
