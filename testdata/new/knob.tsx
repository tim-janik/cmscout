import { LitElement, html, css } from 'lit';
import { customElement } from 'lit/decorators.js';

export const VERSION = '1.0.1';

export class Knob extends LitElement {
  static styles = css`
    .knob { width: 120px; height: 120px; }
  `;

  private last_ = 0;
  private delta = 0;
  private enabled = true;
  private snapped = false;

  connectedCallback() {
    super.connectedCallback();
    this.last_ = 0;
  }

  disconnectedCallback() {
    super.disconnectedCallback();
  }

  connectedCallbackUpdated() {
    this.handleUpdate();
  }

  private handleUpdate() {
    if (this.enabled) {
      this.last_ += this.delta;
      this.notifyValueChanged(this.last_.toString());
    }
  }

  notifyValueChanged(value) {
    this.dispatchEvent(new CustomEvent('value-change', { detail: { value } }));
  }

  render() {
    return html`<div class="knob">${this.last_}</div>`;
  }
}
