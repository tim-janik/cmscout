import { LitElement, html, css } from 'lit';

class MyComponent extends LitElement {
  private state_: Record<string, unknown> = {};

  connectedCallback() {
    super.connectedCallback();
  }

  onSave() {
    this.store();
  }

  private store() {
    const val = this.state_.value;
    localStorage.setItem('key', val);
  }

  render() {
    return html`<div>${this.state_.value}</div>`;
  }
}
