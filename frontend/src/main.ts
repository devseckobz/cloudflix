import { mount } from 'svelte'
import App from './App.svelte'
import './theme.css'
import './app.css'
import './redesign.css'

mount(App, { target: document.getElementById('app')! })
