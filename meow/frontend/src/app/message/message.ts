import { Component, Input } from '@angular/core';
import { DatePipe } from '@angular/common';

@Component({
  selector: 'app-message',
  templateUrl: './message.html',
  styleUrls: ['./message.css'],
  imports: [DatePipe]
})
export class MessageComponent {
  @Input() message: any;
}
