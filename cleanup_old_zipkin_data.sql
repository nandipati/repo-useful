drop event if exists `zipkin_cleanup`; 
DELIMITER $$
CREATE EVENT `zipkin_cleanup`
  ON SCHEDULE EVERY 1 DAY STARTS CURRENT_TIMESTAMP
  ON COMPLETION PRESERVE
DO BEGIN
    DELETE LOW_PRIORITY FROM zipkin.zipkin_spans WHERE start_ts < (UNIX_TIMESTAMP(DATE_SUB(NOW(), INTERVAL 7 DAY)) * 1000 * 1000);
	DELETE LOW_PRIORITY FROM zipkin.zipkin_annotations WHERE a_timestamp < (UNIX_TIMESTAMP(DATE_SUB(NOW(), INTERVAL 7 DAY)) * 1000 * 1000);
	DELETE LOW_PRIORITY FROM zipkin.zipkin_dependencies  WHERE day < DATE_SUB(NOW(), INTERVAL 7 DAY);
END$$
DELIMITER ; -- sets the delimiter back to what we are used to, the semi-colon
