import { LitElement, html, css } from 'lit';

class MyComponent extends LitElement {
  private data_: Record<string, unknown> = {};

  connectedCallback() {
    super.connectedCallback();
  }

  handleSave() {
    this.persist();
  }

  private persist() {
    const val = this.data_.value;
    localStorage.setItem('key', val);
  }

  render() {
    return html`<div>${this.data_.value}</div>`;
  }
}
